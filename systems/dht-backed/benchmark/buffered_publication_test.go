package benchmark

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

func TestBufferedPublicationExactFileProofEquivalence(t *testing.T) {
	for _, capacity := range []uint64{2, 13, 1000} {
		levels := fairTreeLevels(capacity)
		for _, count := range []uint64{0, 1, capacity - 1, capacity} {
			for _, stride := range []uint64{0, min(2, levels), levels} {
				for _, window := range []uint64{4, 1024} {
					t.Run(fmt.Sprintf("capacity%d/count%d/stride%d/window%d", capacity, count, stride, window), func(t *testing.T) {
						cfg := mktree.Config{EpochCapacity: capacity, Stride: stride, Window: window, LogTypes: []string{"access"}}
						leaves := make([]hashing.Digest, count)
						var raw bytes.Buffer
						for i := range leaves {
							leaves[i] = hashing.H([]byte(fmt.Sprintf("leaf%d", i)))
							raw.Write(leaves[i][:])
						}
						before := append([]hashing.Digest(nil), leaves...)
						dir := t.TempDir()
						path := filepath.Join(dir, "leaves.bin")
						if err := os.WriteFile(path, raw.Bytes(), 0600); err != nil {
							t.Fatal(err)
						}
						oldProof, newProof := make(fairMemoryProofSink), make(fairMemoryProofSink)
						want, err := buildFairSparseTree(dir, path, count, 7, "access", cfg, 913, oldProof)
						if err != nil {
							t.Fatal(err)
						}
						got, err := buildBufferedSparseTree(leaves, 7, "access", cfg, 913, newProof)
						if err != nil {
							t.Fatal(err)
						}
						if got != want || !reflect.DeepEqual(oldProof, newProof) {
							t.Fatalf("root or serialized proof bytes differ: old=%x new=%x old objects=%d new objects=%d", want, got, len(oldProof), len(newProof))
						}
						if !bytes.Equal(raw.Bytes(), digestBytes(leaves)) || (len(leaves) > 0 && !reflect.DeepEqual(before, leaves)) {
							t.Fatal("borrowed durable leaf buffer mutated")
						}
					})
				}
			}
		}
	}
}

func digestBytes(leaves []hashing.Digest) []byte {
	var out []byte
	for _, leaf := range leaves {
		out = append(out, leaf[:]...)
	}
	return out
}

func TestBufferedPublicationRejectsInvalidGeometry(t *testing.T) {
	for _, cfg := range []mktree.Config{{EpochCapacity: 2, Stride: 2, Window: 4}, {EpochCapacity: 2, Stride: 1, Window: 0}, {EpochCapacity: 1, Stride: 0, Window: 4}} {
		if _, err := buildBufferedSparseTree(make([]hashing.Digest, 2), 1, "access", cfg, 1, make(fairMemoryProofSink)); err == nil {
			t.Fatal("invalid geometry accepted", cfg)
		}
	}
	if _, err := buildBufferedSparseTree(make([]hashing.Digest, bufferedEpochRecords+1), 1, "access", epochConfig(8192), 1, make(fairMemoryProofSink)); err == nil {
		t.Fatal("buffer bound ignored")
	}
}

func BenchmarkBufferedPublicationBuilder(b *testing.B) {
	leaves := make([]hashing.Digest, 1000)
	var raw bytes.Buffer
	for i := range leaves {
		leaves[i] = hashing.H([]byte(fmt.Sprintf("leaf%d", i)))
		raw.Write(leaves[i][:])
	}
	cfg := epochConfig(1000)
	b.Run("file_reference", func(b *testing.B) {
		dir := b.TempDir()
		path := filepath.Join(dir, "leaves.bin")
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := os.WriteFile(path, raw.Bytes(), 0600); err != nil {
				b.Fatal(err)
			}
			if _, err := buildFairSparseTree(dir, path, 1000, 7, "access", cfg, 1000, make(fairMemoryProofSink)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("buffered", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := buildBufferedSparseTree(leaves, 7, "access", cfg, 1000, make(fairMemoryProofSink)); err != nil {
				b.Fatal(err)
			}
		}
	})
}
