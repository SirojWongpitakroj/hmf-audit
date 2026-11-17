package benchmark

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhtbacked/auth"
	"dhtbacked/blockchain"
	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/ingestverifier"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
)

const (
	fairDHTEpoch         = uint64(1)
	fairDHTCopyBatchSize = 4096
	// fairDHTSeedWriters bounds the concurrent Cassandra writes that load the
	// proof records during setup.
	fairDHTSeedWriters = 32
	fairDHTScannerMax  = 4 << 20
)

var fairDHTPlacements = []string{"clustered-2", "scattered"}
var fairDHTRequestCounts = []int{1, 16, 64, 256, 1024, 4096, 16384}

type fairDHTDatasetRecord struct {
	SourceID string `json:"source_id"`
	RawLog   string `json:"raw_log"`
	RegionID string `json:"region_id"`
	TenantID string `json:"tenant_id"`
}

type fairDHTCase struct {
	placement string
	count     int
}

// fairDHTPersistentFixture is deliberately small: encrypted tuples live in
// PostgreSQL and proof records live in the Cassandra-backed DHT.
type fairDHTPersistentFixture struct {
	config  mktree.Config
	ca      *auth.CA
	chain   *blockchain.Memory
	batches map[fairDHTCase][]string
}

type fairDHTAuthorities struct {
	encryptor *encryptor.Encryptor
	gate      *ingestverifier.Verifier
	private   map[string]ed25519.PrivateKey
	ca        *auth.CA
}

func newFairDHTAuthorities(workDir string) (*fairDHTAuthorities, error) {
	enc, err := encryptor.New(make([]byte, encryptor.KeyLen))
	if err != nil {
		return nil, err
	}
	keys, err := producer.LoadOrCreateKeys(filepath.Join(workDir, "producers"))
	if err != nil {
		return nil, err
	}
	entries := make(map[string]ed25519.PublicKey, len(keys))
	private := make(map[string]ed25519.PrivateKey, len(keys))
	for _, key := range keys {
		entries[key.ID] = key.Public
		private[key.ID] = key.Private
	}
	registryPath := filepath.Join(workDir, "registry.json")
	if err := ingestverifier.WriteRegistry(registryPath, entries); err != nil {
		return nil, err
	}
	registry, err := ingestverifier.LoadRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	caPrivate, _, err := auth.LoadOrCreateCAKeys(
		filepath.Join(workDir, "ca.sk"), filepath.Join(workDir, "ca.pk"),
	)
	if err != nil {
		return nil, err
	}
	ca, err := auth.NewCA(caPrivate, clock.System{})
	if err != nil {
		return nil, err
	}
	return &fairDHTAuthorities{
		encryptor: enc,
		gate:      ingestverifier.New(registry),
		private:   private,
		ca:        ca,
	}, nil
}

type fairDigestSpool struct {
	paths   map[string]string
	files   map[string]*os.File
	writers map[string]*bufio.Writer
}

func newFairDigestSpool(directory string, logTypes []string) (*fairDigestSpool, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	spool := &fairDigestSpool{
		paths:   make(map[string]string, len(logTypes)),
		files:   make(map[string]*os.File, len(logTypes)),
		writers: make(map[string]*bufio.Writer, len(logTypes)),
	}
	for _, logType := range logTypes {
		path := filepath.Join(directory, "leaves-"+logType+".bin")
		file, err := os.Create(path)
		if err != nil {
			_ = spool.Close()
			return nil, err
		}
		spool.paths[logType] = path
		spool.files[logType] = file
		spool.writers[logType] = bufio.NewWriterSize(file, 1<<20)
	}
	return spool, nil
}

func (s *fairDigestSpool) Append(logType string, digest hashing.Digest) error {
	writer, ok := s.writers[logType]
	if !ok {
		return fmt.Errorf("fair DHT: no digest spool for log type %q", logType)
	}
	_, err := writer.Write(digest[:])
	return err
}

func (s *fairDigestSpool) Close() error {
	var first error
	for logType, writer := range s.writers {
		if err := writer.Flush(); err != nil && first == nil {
			first = fmt.Errorf("flush %s digest spool: %w", logType, err)
		}
	}
	for logType, file := range s.files {
		if err := file.Close(); err != nil && first == nil {
			first = fmt.Errorf("close %s digest spool: %w", logType, err)
		}
	}
	s.writers = nil
	s.files = nil
	return first
}

type fairProofSink interface {
	Put(key hashing.Digest, payload []byte) error
}

func countFairDHTRecords(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), fairDHTScannerMax)
	var count uint64
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, fmt.Errorf("fair DHT: shared dataset is empty")
	}
	return count, nil
}

func buildFairDHTPersistentFixture(
	ctx context.Context,
	datasetPath string,
	segmentLeaves int,
	workDir string,
	store *storage.Postgres,
	proofStore *cassandradht.Cassandra,
	peers int,
) (*fairDHTPersistentFixture, error) {
	recordCount, err := countFairDHTRecords(datasetPath)
	if err != nil {
		return nil, fmt.Errorf("count shared dataset: %w", err)
	}
	maxInt := uint64(^uint(0) >> 1)
	if recordCount > maxInt {
		return nil, fmt.Errorf("fair DHT: %d records exceed this platform's int range", recordCount)
	}
	config := mktree.DefaultConfig()
	config.EpochCapacity = recordCount
	config.LogTypes = append([]string(nil), config.LogTypes...)
	sort.Strings(config.LogTypes)
	batches, err := fairDHTSelectedBatches(int(recordCount), segmentLeaves)
	if err != nil {
		return nil, err
	}

	if err := store.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate PostgreSQL store: %w", err)
	}
	if err := store.Reset(ctx); err != nil {
		return nil, fmt.Errorf("reset PostgreSQL store: %w", err)
	}
	if err := proofStore.Reset(ctx); err != nil {
		return nil, fmt.Errorf("reset Cassandra proof store: %w", err)
	}
	if err := proofStore.SeedRoutes(ctx, peers); err != nil {
		return nil, fmt.Errorf("seed Cassandra routing table: %w", err)
	}

	authorities, err := newFairDHTAuthorities(workDir)
	if err != nil {
		return nil, fmt.Errorf("initialize DHT authorities: %w", err)
	}
	cacheDir := filepath.Join(workDir, "tree-cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(cacheDir)

	spool, err := newFairDigestSpool(cacheDir, config.LogTypes)
	if err != nil {
		return nil, err
	}
	leafCounts, ingestErr := ingestFairDHTDataset(
		ctx, datasetPath, recordCount, authorities, store, spool,
	)
	closeErr := spool.Close()
	if ingestErr != nil {
		return nil, ingestErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	proofs := proofStore.NewSeeder(ctx, fairDHTSeedWriters)
	defer func() { _ = proofs.Flush() }() // stops the writers on an early error; Flush is idempotent
	roots := make([]hashing.Digest, 0, len(config.LogTypes))
	for _, logType := range config.LogTypes {
		root, err := buildFairSparseTree(
			cacheDir,
			spool.paths[logType],
			leafCounts[logType],
			fairDHTEpoch,
			logType,
			config,
			recordCount,
			proofs,
		)
		if err != nil {
			return nil, fmt.Errorf("build sparse %s tree: %w", logType, err)
		}
		roots = append(roots, root)
	}
	if err := proofs.Flush(); err != nil {
		return nil, err
	}

	chain := blockchain.NewMemory()
	if err := chain.Commit(ctx, fairDHTEpoch, mktree.RootOfRoots(roots)); err != nil {
		return nil, err
	}
	return &fairDHTPersistentFixture{
		config:  config,
		ca:      authorities.ca,
		chain:   chain,
		batches: batches,
	}, nil
}

func ingestFairDHTDataset(
	ctx context.Context,
	path string,
	expected uint64,
	authorities *fairDHTAuthorities,
	store *storage.Postgres,
	spool *fairDigestSpool,
) (map[string]uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	counts := make(map[string]uint64, len(spool.paths))
	batch := make([]storage.Record, 0, fairDHTCopyBatchSize)
	flush := func() error {
		if err := store.CopyRecords(ctx, batch); err != nil {
			return err
		}
		clear(batch)
		batch = batch[:0]
		return nil
	}

	region := -1
	var index uint64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), fairDHTScannerMax)
	for scanner.Scan() {
		var input fairDHTDatasetRecord
		if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
			return nil, fmt.Errorf("decode shared dataset record %d: %w", index, err)
		}
		if input.SourceID == "" || input.RawLog == "" || input.TenantID == "" {
			return nil, fmt.Errorf("invalid shared dataset record %d", index)
		}
		if input.RegionID != fmt.Sprintf("R%d", region) {
			if input.RegionID != fmt.Sprintf("R%d", region+1) {
				return nil, fmt.Errorf(
					"shared dataset record %d has region %q; records must be grouped R0, R1, ... in order",
					index, input.RegionID,
				)
			}
			region++
		}

		sourceDigest := sha256.Sum256([]byte(input.SourceID))
		logType := producer.LogTypes[int(sourceDigest[0])%len(producer.LogTypes)]
		producerID := producer.IDForLogType(logType)
		privateKey, ok := authorities.private[producerID]
		if !ok {
			return nil, fmt.Errorf("fair DHT: no signing key for %s", producerID)
		}
		record := encryptor.Record{
			RecordID:       encryptor.FormatRecordID(index),
			ProducerID:     producerID,
			LogType:        logType,
			TimestampNanos: fairDHTTimestamp(input.RawLog, index),
			Message:        input.RawLog,
			Attributes: encryptor.Attributes{
				Host: input.RegionID, RequestID: input.SourceID,
				SourceIP: input.TenantID, Status: "openstack",
			},
		}
		emitted, err := authorities.encryptor.ProcessRecord(record, privateKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt record %d: %w", index, err)
		}
		if err := authorities.gate.Validate(emitted); err != nil {
			return nil, fmt.Errorf("validate record %d: %w", index, err)
		}
		leafIndex := counts[logType]
		counts[logType]++
		batch = append(batch, storage.Record{
			RecordID:          record.RecordID,
			Epoch:             fairDHTEpoch,
			LogType:           logType,
			LeafIndex:         leafIndex,
			ProducerID:        producerID,
			TimestampNanos:    record.TimestampNanos,
			C:                 emitted.C,
			T:                 emitted.T,
			AD:                emitted.AD,
			D:                 emitted.D,
			Sigma:             emitted.Sigma,
			ProducerPublicKey: emitted.ProducerPublicKey,
		})
		if err := spool.Append(logType, emitted.D); err != nil {
			return nil, err
		}
		if len(batch) >= cap(batch) {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if index != expected {
		return nil, fmt.Errorf("fair DHT: ingested %d records, counted %d", index, expected)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return counts, nil
}

func fairDHTTimestamp(raw string, index uint64) int64 {
	fields := strings.Fields(raw)
	if len(fields) >= 3 {
		value := fields[1] + " " + fields[2]
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
			if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
				return parsed.UnixNano()
			}
		}
	}
	return time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC).
		Add(time.Duration(index) * time.Millisecond).UnixNano()
}

func fairTreeLevels(capacity uint64) uint64 {
	width := mktree.PaddedWidth(capacity)
	var levels uint64
	for width > 1 {
		width >>= 1
		levels++
	}
	return levels
}

func fairDummyNodes(levels uint64) []hashing.Digest {
	dummy := make([]hashing.Digest, levels+1)
	dummy[levels] = mktree.DummyLeaf()
	for level := levels; level > 0; level-- {
		dummy[level-1] = mktree.Internal(dummy[level], dummy[level])
	}
	return dummy
}

func buildFairSparseTree(
	workDir string,
	leafPath string,
	leafCount uint64,
	epoch uint64,
	logType string,
	config mktree.Config,
	commitSeq uint64,
	sink fairProofSink,
) (hashing.Digest, error) {
	levels := fairTreeLevels(config.EpochCapacity)
	if config.Stride > levels {
		return hashing.Digest{}, fmt.Errorf("stride %d exceeds tree level %d", config.Stride, levels)
	}
	treeID := dht.TreeID(epoch, logType)
	dummy := fairDummyNodes(levels)
	if err := persistFairMicroRoots(
		leafPath, leafCount, epoch, logType, treeID, config.Window, commitSeq, sink,
	); err != nil {
		return hashing.Digest{}, err
	}

	currentPath := leafPath
	active := leafCount
	for level := levels; level > config.Stride; level-- {
		nextPath := filepath.Join(workDir, fmt.Sprintf("nodes-%s-%d.bin", logType, level-1))
		nextActive, err := reduceFairSparseLevel(
			currentPath, nextPath, active, level, config.Stride,
			treeID, commitSeq, dummy[level], sink,
		)
		if err != nil {
			return hashing.Digest{}, err
		}
		if err := os.Remove(currentPath); err != nil && !os.IsNotExist(err) {
			return hashing.Digest{}, err
		}
		currentPath = nextPath
		active = nextActive
	}

	upper, err := loadFairUpperTree(currentPath, active, config.Stride, dummy)
	if err != nil {
		return hashing.Digest{}, err
	}
	if err := os.Remove(currentPath); err != nil && !os.IsNotExist(err) {
		return hashing.Digest{}, err
	}
	return publishFairUpperTree(upper, active, leafCount, epoch, logType, config, commitSeq, sink)
}

// Shared encoding ensures the buffered and file-backed builders publish the
// same ancestor certificates and root records.
func publishFairUpperTree(upper [][]hashing.Digest, active, leafCount, epoch uint64, logType string, config mktree.Config, commitSeq uint64, sink fairProofSink) (hashing.Digest, error) {
	treeID := dht.TreeID(epoch, logType)
	levels := fairTreeLevels(config.EpochCapacity)
	root := upper[0][0]
	for ancestor := uint64(0); ancestor < active; ancestor++ {
		upProof := make([]hashing.Digest, 0, config.Stride)
		index := ancestor
		for level := config.Stride; level > 0; level-- {
			upProof = append(upProof, upper[level][index^1])
			index >>= 1
		}
		iMin, iMax := mktree.AncestorRange(ancestor, levels, config.Stride)
		certificate := dht.AncestorCert{
			AncestorID:   dht.AncestorID(treeID, config.Stride, ancestor),
			TreeID:       treeID,
			Level:        config.Stride,
			Index:        ancestor,
			AncestorHash: upper[config.Stride][ancestor],
			UpProof:      upProof,
			IMin:         iMin,
			IMax:         iMax,
			CommitSeq:    commitSeq,
		}
		if err := sink.Put(dht.KeyC(treeID, config.Stride, ancestor), certificate.Serialize()); err != nil {
			return hashing.Digest{}, err
		}
	}
	rootRecord := dht.RootRecord{
		TreeID: treeID, Seq: commitSeq, Root: root, LeafCnt: leafCount, LogType: logType,
	}
	if err := sink.Put(dht.KeyR(treeID), rootRecord.Serialize()); err != nil {
		return hashing.Digest{}, err
	}
	return root, nil
}

func reduceFairSparseLevel(
	currentPath string,
	nextPath string,
	active uint64,
	level uint64,
	stride uint64,
	treeID hashing.Digest,
	commitSeq uint64,
	dummy hashing.Digest,
	sink fairProofSink,
) (nextActive uint64, resultErr error) {
	current, err := os.Open(currentPath)
	if err != nil {
		return 0, err
	}
	defer current.Close()
	next, err := os.Create(nextPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := next.Close(); resultErr == nil && closeErr != nil {
			resultErr = closeErr
		}
		if resultErr != nil {
			_ = os.Remove(nextPath)
		}
	}()

	reader := bufio.NewReaderSize(current, 1<<20)
	writer := bufio.NewWriterSize(next, 1<<20)
	totalNodes := uint64(1) << level
	for index := uint64(0); index < active; index += 2 {
		left, err := readFairDigest(reader)
		if err != nil {
			return 0, fmt.Errorf("read level %d node %d: %w", level, index, err)
		}
		if level > stride {
			parent := dht.ParentNode{TreeID: treeID, J: level, K: index, Node: left, CommitSeq: commitSeq}
			if err := sink.Put(dht.KeyP(treeID, level, index), parent.Serialize()); err != nil {
				return 0, err
			}
		}

		right := dummy
		if index+1 < active {
			right, err = readFairDigest(reader)
			if err != nil {
				return 0, fmt.Errorf("read level %d node %d: %w", level, index+1, err)
			}
			if level > stride {
				parent := dht.ParentNode{TreeID: treeID, J: level, K: index + 1, Node: right, CommitSeq: commitSeq}
				if err := sink.Put(dht.KeyP(treeID, level, index+1), parent.Serialize()); err != nil {
					return 0, err
				}
			}
		} else if index+1 < totalNodes && level > stride {
			parent := dht.ParentNode{TreeID: treeID, J: level, K: index + 1, Node: dummy, CommitSeq: commitSeq}
			if err := sink.Put(dht.KeyP(treeID, level, index+1), parent.Serialize()); err != nil {
				return 0, err
			}
		}
		combined := mktree.Internal(left, right)
		if _, err := writer.Write(combined[:]); err != nil {
			return 0, err
		}
	}
	if err := ensureFairDigestEOF(reader); err != nil {
		return 0, err
	}
	if err := writer.Flush(); err != nil {
		return 0, err
	}
	return (active + 1) / 2, nil
}

func loadFairUpperTree(
	path string,
	active uint64,
	stride uint64,
	dummy []hashing.Digest,
) ([][]hashing.Digest, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	upper := make([][]hashing.Digest, stride+1)
	upper[stride] = make([]hashing.Digest, uint64(1)<<stride)
	for index := range upper[stride] {
		upper[stride][index] = dummy[stride]
	}
	for index := uint64(0); index < active; index++ {
		digest, err := readFairDigest(reader)
		if err != nil {
			return nil, fmt.Errorf("read stride node %d: %w", index, err)
		}
		upper[stride][index] = digest
	}
	if err := ensureFairDigestEOF(reader); err != nil {
		return nil, err
	}
	for level := stride; level > 0; level-- {
		upper[level-1] = make([]hashing.Digest, len(upper[level])/2)
		for index := range upper[level-1] {
			upper[level-1][index] = mktree.Internal(
				upper[level][2*index], upper[level][2*index+1],
			)
		}
	}
	return upper, nil
}

func persistFairMicroRoots(
	path string,
	leafCount uint64,
	epoch uint64,
	logType string,
	treeID hashing.Digest,
	window uint64,
	commitSeq uint64,
	sink fairProofSink,
) error {
	if leafCount == 0 {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 1<<20)
	for windowID, first := uint64(0), uint64(0); first < leafCount; windowID, first = windowID+1, first+window {
		last := first + window
		if last > leafCount {
			last = leafCount
		}
		digests := make([]hashing.Digest, 0, last-first)
		for index := first; index < last; index++ {
			digest, err := readFairDigest(reader)
			if err != nil {
				return err
			}
			digests = append(digests, digest)
		}
		record := dht.MicroRootRecord{
			TreeID: treeID, WindowID: windowID, IMin: first, IMax: last - 1,
			MicroRoot: mktree.MicroRoot(digests), Seq: commitSeq,
		}
		if err := sink.Put(dht.KeyM(treeID, windowID), record.Serialize()); err != nil {
			return err
		}
	}
	return ensureFairDigestEOF(reader)
}

func readFairDigest(reader io.Reader) (hashing.Digest, error) {
	var digest hashing.Digest
	_, err := io.ReadFull(reader, digest[:])
	return digest, err
}

func ensureFairDigestEOF(reader *bufio.Reader) error {
	_, err := reader.ReadByte()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("fair DHT: digest spool contains trailing bytes")
}

func fairDHTClusteredSegments(placement string) (int, bool) {
	switch placement {
	case "clustered-2":
		return 2, true
	case "clustered-3":
		return 3, true
	case "clustered-4":
		return 4, true
	default:
		return 0, false
	}
}

func fairDHTBatchPositions(total int, segmentLeaves int, placement string, count int) ([]int, error) {
	if regionText := os.Getenv("FAIR_REGION0_RECORDS"); regionText != "" {
		region0, err := strconv.Atoi(regionText)
		if err != nil || region0 <= 0 || region0 > total {
			return nil, fmt.Errorf("invalid FAIR_REGION0_RECORDS %q", regionText)
		}
		region1Text := os.Getenv("FAIR_REGION1_RECORDS")
		region1, err := strconv.Atoi(region1Text)
		if err != nil || region1 <= 0 || region0+region1 > total {
			return nil, fmt.Errorf("invalid FAIR_REGION1_RECORDS %q", region1Text)
		}
		start, width := 0, 2*segmentLeaves
		switch placement {
		case "clustered-2":
		case "scattered":
			start, width = region0, total-region0
		default:
			return nil, fmt.Errorf("unknown fair DHT placement: %s", placement)
		}
		if count <= 0 || count > width || 2*segmentLeaves > region0 {
			return nil, fmt.Errorf("invalid %s q=%d for R0=%d", placement, count, region0)
		}
		bitCount := bits.Len(uint(width - 1))
		positions := make([]int, 0, count)
		for index := uint64(0); len(positions) < count; index++ {
			offset := int(bits.Reverse64(index) >> (64 - bitCount))
			if offset < width {
				positions = append(positions, start+offset)
			}
		}
		return positions, nil
	}
	clusteredSegments, isClustered := fairDHTClusteredSegments(placement)
	clusteredWindow := clusteredSegments * segmentLeaves
	if count <= 0 || count > total {
		return nil, fmt.Errorf("invalid fair DHT count %d for %d records", count, total)
	}
	if isClustered && count > 1 &&
		(count < 16 || clusteredWindow > total || count > clusteredWindow) {
		return nil, fmt.Errorf("invalid %s q=%d, segment leaves=%d, total=%d",
			placement, count, segmentLeaves, total)
	}
	positions := make([]int, 0, count)
	for index := 0; index < count; index++ {
		position := 0
		switch {
		case isClustered && count > 1:
			position = index * (clusteredWindow - 1) / (count - 1)
		case isClustered:
		case placement == "scattered" && count > 1:
			position = index * (total - 1) / (count - 1)
		case placement == "scattered":
		default:
			return nil, fmt.Errorf("unknown fair DHT placement: %s", placement)
		}
		positions = append(positions, position)
	}
	return positions, nil
}

func fairDHTSelectedBatches(total int, segmentLeaves int) (map[fairDHTCase][]string, error) {
	batches := make(map[fairDHTCase][]string, len(fairDHTPlacements)*len(fairDHTRequestCounts))
	for _, placement := range fairDHTPlacements {
		for _, count := range fairDHTRequestCounts {
			positions, err := fairDHTBatchPositions(total, segmentLeaves, placement, count)
			if err != nil {
				return nil, err
			}
			ids := make([]string, len(positions))
			for index, position := range positions {
				ids[index] = encryptor.FormatRecordID(uint64(position))
			}
			batches[fairDHTCase{placement: placement, count: count}] = ids
		}
	}
	return batches, nil
}
