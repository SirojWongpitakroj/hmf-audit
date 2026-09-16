package lsg

import (
	"strings"
	"sync"
	"testing"
	"time"

	"delia/hash"
	"delia/logcache"
)

// TEST-01-style (self-store half, LSG-004): a server stores its own
// generated record hash in its own LogCacheQueue.
func TestLogReaderStoresOwnHashes(t *testing.T) {
	store, err := logcache.New(4)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var batches [][]hash.Digest
	done := make(chan struct{})

	reader := NewLogReader("p1", store, func(batch []hash.Digest) {
		mu.Lock()
		batches = append(batches, batch)
		mu.Unlock()
		if len(batch) > 0 {
			close(done)
		}
	})

	lines := "d1\nd2\nd3\nd4\n"
	reader.Start(strings.NewReader(lines))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for batch completion")
	}
	reader.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 1 {
		t.Fatalf("got %d completed batches, want 1", len(batches))
	}
	want := []hash.Digest{
		hash.H([]byte("d1")),
		hash.H([]byte("d2")),
		hash.H([]byte("d3")),
		hash.H([]byte("d4")),
	}
	for i, d := range want {
		if batches[0][i] != d {
			t.Fatalf("batch[%d] mismatch", i)
		}
	}
}

func TestLogReaderStartStopLifecycle(t *testing.T) {
	// SYS-007/DEV-08: "enable" starts the reader, "close" stops it; no
	// separate collector/preprocessor/verifier components exist.
	store, _ := logcache.New(100)
	reader := NewLogReader("p1", store, nil)
	reader.Start(strings.NewReader("only one line\n"))
	reader.Stop()
	// Stop must be idempotent and must not hang or panic.
	reader.Stop()
}

func TestLogReaderExcludesLineEndings(t *testing.T) {
	// GAP-03(a): a record is the UTF-8 line, excluding line-ending bytes.
	store, _ := logcache.New(1)
	done := make(chan []hash.Digest, 1)
	reader := NewLogReader("p1", store, func(batch []hash.Digest) {
		done <- batch
	})
	reader.Start(strings.NewReader("hello\r\n"))
	select {
	case batch := <-done:
		want := hash.H([]byte("hello"))
		if batch[0] != want {
			t.Fatalf("record hash included the line ending")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out")
	}
	reader.Stop()
}

func TestLogReaderNoPartialBatchTrigger(t *testing.T) {
	store, _ := logcache.New(4)
	var calls int
	var mu sync.Mutex
	reader := NewLogReader("p1", store, func(batch []hash.Digest) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	reader.Start(strings.NewReader("a\nb\n"))
	reader.Stop()
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("onFull called %d times for a partial (2-record) batch of theta=4", calls)
	}
}
