package benchmark

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

type fairMemoryProofSink map[hashing.Digest][]byte

func (s fairMemoryProofSink) Put(key hashing.Digest, payload []byte) error {
	s[key] = append([]byte(nil), payload...)
	return nil
}

func TestBuildFairSparseTreeMatchesDenseTree(t *testing.T) {
	config := mktree.Config{
		EpochCapacity: 13,
		Stride:        2,
		Window:        4,
		LogTypes:      []string{"access"},
	}
	const leafCount = uint64(11)
	const commitSeq = uint64(73)
	const logType = "access"

	dense, err := mktree.NewTree(fairDHTEpoch, logType, config.EpochCapacity, config.Stride)
	if err != nil {
		t.Fatal(err)
	}
	digests := make([]hashing.Digest, 0, leafCount)
	var spool bytes.Buffer
	for index := uint64(0); index < leafCount; index++ {
		digest := hashing.H([]byte(fmt.Sprintf("leaf-%d", index)))
		digests = append(digests, digest)
		if _, _, err := dense.Insert(digest); err != nil {
			t.Fatal(err)
		}
		spool.Write(digest[:])
	}

	directory := t.TempDir()
	leafPath := filepath.Join(directory, "leaves-access.bin")
	if err := os.WriteFile(leafPath, spool.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	sink := make(fairMemoryProofSink)
	root, err := buildFairSparseTree(
		directory, leafPath, leafCount, fairDHTEpoch, logType,
		config, commitSeq, sink,
	)
	if err != nil {
		t.Fatal(err)
	}
	if root != dense.Root() {
		t.Fatalf("sparse root %x, dense root %x", root, dense.Root())
	}
	if _, err := os.Stat(leafPath); !os.IsNotExist(err) {
		t.Fatalf("tree cache was not discarded: stat error %v", err)
	}

	treeID := dense.TreeID()
	for leaf := uint64(0); leaf < leafCount; leaf++ {
		index := leaf
		for level := dense.Levels(); level > config.Stride; level-- {
			sibling := index ^ 1
			key := dht.KeyP(treeID, level, sibling)
			raw, ok := sink[key]
			if !ok {
				t.Fatalf("missing P_%d,%d needed by leaf %d", level, sibling, leaf)
			}
			got, err := dht.ParseParentNode(raw)
			if err != nil {
				t.Fatal(err)
			}
			wantNode, err := dense.Node(level, sibling)
			if err != nil {
				t.Fatal(err)
			}
			if got.TreeID != treeID || got.J != level || got.K != sibling ||
				got.Node != wantNode || got.CommitSeq != commitSeq {
				t.Fatalf("P_%d,%d does not match the dense tree", level, sibling)
			}
			index >>= 1
		}

		ancestor := dense.CoveringAncestor(leaf)
		key := dht.KeyC(treeID, config.Stride, ancestor)
		want, err := dense.Certificate(ancestor, commitSeq)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := sink[key]; !ok {
			t.Fatalf("missing certificate %d needed by leaf %d", ancestor, leaf)
		} else if !bytes.Equal(got, want.Serialize()) {
			t.Fatalf("certificate %d does not match the dense tree", ancestor)
		}
	}

	rootKey := dht.KeyR(treeID)
	wantRoot := dht.RootRecord{
		TreeID: treeID, Seq: commitSeq, Root: dense.Root(), LeafCnt: leafCount, LogType: logType,
	}
	if got := sink[rootKey]; !bytes.Equal(got, wantRoot.Serialize()) {
		t.Fatal("root record does not match the dense tree")
	}
	for windowID, first := uint64(0), uint64(0); first < leafCount; windowID, first = windowID+1, first+config.Window {
		last := first + config.Window
		if last > leafCount {
			last = leafCount
		}
		want := dht.MicroRootRecord{
			TreeID: treeID, WindowID: windowID, IMin: first, IMax: last - 1,
			MicroRoot: mktree.MicroRoot(digests[first:last]), Seq: commitSeq,
		}
		if got := sink[dht.KeyM(treeID, windowID)]; !bytes.Equal(got, want.Serialize()) {
			t.Fatalf("micro-root %d does not match the dense tree", windowID)
		}
	}

	if _, ok := sink[dht.KeyC(treeID, config.Stride, 3)]; ok {
		t.Fatal("dummy-only ancestor was persisted")
	}
	if _, ok := sink[dht.KeyP(treeID, dense.Levels(), 14)]; ok {
		t.Fatal("dummy-only leaf node was persisted")
	}
}

func TestBuildSparseTreeSingleRecordFinalEpoch(t *testing.T) {
	for _, leaves := range []uint64{0, 1} {
		cfg := mktree.Config{EpochCapacity: 2, Stride: 1, Window: 1024, LogTypes: []string{"access"}}
		dense, err := mktree.NewTree(4, "access", cfg.EpochCapacity, cfg.Stride)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "leaves-access.bin")
		var raw []byte
		if leaves == 1 {
			digest := hashing.H([]byte("one"))
			if _, _, err := dense.Insert(digest); err != nil {
				t.Fatal(err)
			}
			raw = digest[:]
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := buildFairSparseTree(filepath.Dir(path), path, leaves, 4, "access", cfg, 1, make(fairMemoryProofSink))
		if err != nil {
			t.Fatal(err)
		}
		if root != dense.Root() {
			t.Fatalf("leaves=%d: sparse root differs from dense", leaves)
		}
	}
}

func TestFairDHTSelectedBatchesRetainOnlyRequestedIDs(t *testing.T) {
	const total = 100_000
	const segmentLeaves = 8192
	batches, _, err := fairDHTSelectedBatches(total, segmentLeaves)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(batches), len(fairDHTPlacements)*len(fairDHTRequestCounts); got != want {
		t.Fatalf("batch count %d, want %d", got, want)
	}
	for _, placement := range fairDHTPlacements {
		ids := batches[fairDHTCase{placement: placement, count: 1}]
		if len(ids) != 1 || ids[0] != encryptor.FormatRecordID(0) {
			t.Fatalf("%s q=1 = %v", placement, ids)
		}
	}
	clustered := batches[fairDHTCase{placement: "clustered-2", count: 4096}]
	if clustered[len(clustered)-1] != encryptor.FormatRecordID(2*segmentLeaves-1) {
		t.Fatalf("clustered-2 last ID = %s", clustered[len(clustered)-1])
	}
	scattered := batches[fairDHTCase{placement: "scattered", count: 4096}]
	if scattered[len(scattered)-1] != encryptor.FormatRecordID(total-1) {
		t.Fatalf("scattered last ID = %s", scattered[len(scattered)-1])
	}
}

func TestFairDHTVerificationSelectionMatchesLogicalALLWorkload(t *testing.T) {
	t.Setenv("FAIR_REGION0_RECORDS", "32768")
	t.Setenv("FAIR_REGION1_RECORDS", "32768")
	for _, placement := range fairDHTPlacements {
		for _, count := range []int{1, 16, 256, 16384} {
			positions, err := fairDHTBatchPositions(98304, 8192, placement, count)
			if err != nil {
				t.Fatal(err)
			}
			if len(positions) != count {
				t.Fatalf("%s/q=%d returned %d positions", placement, count, len(positions))
			}
			if count >= 16 {
				wantPrefix := []int{0, 8192, 4096, 12288}
				if placement == "scattered" {
					wantPrefix = []int{32768, 65536, 49152, 81920}
				}
				for i, want := range wantPrefix {
					if positions[i] != want {
						t.Fatalf("%s/q=%d position %d = %d, want %d", placement, count, i, positions[i], want)
					}
				}
			}
			seen := make(map[int]bool, count)
			for _, position := range positions {
				if seen[position] || placement == "clustered-2" && (position < 0 || position >= 16384) ||
					placement == "scattered" && (position < 32768 || position >= 98304) {
					t.Fatalf("%s/q=%d invalid ordinal %d", placement, count, position)
				}
				seen[position] = true
			}
		}
	}
}
