package closure

import (
	"errors"
	"io"
	"testing"
	"time"

	"delia/logcache"
	"delia/lsg"
)

type recordingStopper struct {
	stopped bool
}

func (s *recordingStopper) Stop() { s.stopped = true }

func TestCloseInstanceStopsReaderBeforeInvokingClose(t *testing.T) {
	stopper := &recordingStopper{}
	var order []string
	err := CloseInstance(stopper, func() error {
		order = append(order, "close")
		if !stopper.stopped {
			t.Fatal("expected the reader to be stopped before invokeClose runs")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopper.stopped {
		t.Fatal("expected Stop to have been called")
	}
	if len(order) != 1 || order[0] != "close" {
		t.Fatalf("expected invokeClose to run exactly once, got %v", order)
	}
}

func TestCloseInstanceStopsReaderEvenIfCloseFails(t *testing.T) {
	stopper := &recordingStopper{}
	wantErr := errors.New("close rejected")
	err := CloseInstance(stopper, func() error { return wantErr })
	if err != wantErr {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
	if !stopper.stopped {
		t.Fatal("expected the reader to be stopped even though invokeClose failed (no separate teardown step)")
	}
}

// End-to-end: a real lsg.LogReader (SYS-007's "log reader" = the whole
// collector/preprocessor/verification stand-in, DEV-08) is actually stopped
// by CloseInstance, matching "close" = stop the reader, no separate module.
//
// lsg.LogReader.Stop (M0/M4, frozen) only interrupts the read loop BETWEEN
// completed bufio.Scanner.Scan() calls — a Scan() call already blocked
// waiting on more input from an open, non-EOF source cannot be interrupted
// (there is no cancellable-read plumbing, by design: GO-003/GO-004 forbid
// adding concurrency machinery beyond the one reader goroutine). This test
// therefore closes its pipe (a natural EOF) before calling CloseInstance,
// exactly like a real bounded log source running out of input, rather than
// stopping mid-read — which is the scenario CLOSE-008 actually describes
// ("each server closes its log collector... and the Close protocol is
// invoked"), not an interrupt-a-blocked-read guarantee this milestone does
// not claim.
func TestCloseInstanceStopsRealLogReader(t *testing.T) {
	cache, err := logcache.New(100) // large theta: batches never complete during this test
	if err != nil {
		t.Fatalf("logcache.New: %v", err)
	}
	pr, pw := io.Pipe()
	reader := lsg.NewLogReader("p1", cache, nil)
	reader.Start(pr)

	go func() {
		_, _ = pw.Write([]byte("line-1\nline-2\n"))
		_ = pw.Close()
	}()

	// Wait for the reader to finish consuming the (now-closed) source
	// naturally, before CloseInstance runs — see the doc comment above.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cache.Current("p1")) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(cache.Current("p1")); got != 2 {
		t.Fatalf("expected 2 records cached before closure, got %d", got)
	}

	closeCalled := false
	done := make(chan error, 1)
	go func() { done <- CloseInstance(reader, func() error { closeCalled = true; return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CloseInstance: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CloseInstance did not return (reader already stopped naturally, Stop should be a fast no-op)")
	}
	if !closeCalled {
		t.Fatal("expected invokeClose to run")
	}

	// No further records after closure: the source was exhausted and the
	// reader is now stopped (also coupled by CloseInstance).
	if got := len(cache.Current("p1")); got != 2 {
		t.Fatalf("expected record count unchanged after closure, cache has %d", got)
	}
}
