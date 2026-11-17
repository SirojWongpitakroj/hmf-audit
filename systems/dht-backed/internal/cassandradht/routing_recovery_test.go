package cassandradht

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

type routingRequestError int

func (e routingRequestError) Code() int { return int(e) }
func (e routingRequestError) Message() string {
	return fmt.Sprintf("Cassandra request error %x", int(e))
}
func (e routingRequestError) Error() string { return e.Message() }

func TestRoutingMetadataResolvesUncertainWrites(t *testing.T) {
	for _, code := range []int{gocql.ErrCodeCASWriteUnknown, gocql.ErrCodeWriteTimeout} {
		t.Run(fmt.Sprintf("%x", code), func(t *testing.T) {
			pending, committed, writes, serialReads := false, false, 0, 0
			err := ensureRoutingMetadata(context.Background(), "route (7,9)", func(_ context.Context, serial bool) error {
				if serial {
					serialReads++
					committed, pending = pending, false // Paxos completes only on a serial read.
				}
				if !committed {
					return gocql.ErrNotFound
				}
				return nil
			}, func(context.Context) error {
				writes++
				pending = true
				return routingRequestError(code)
			})
			if err != nil || writes != 1 || serialReads != 1 || !committed {
				t.Fatalf("lost acknowledgement was not safely resolved: err=%v writes=%d serial=%d committed=%t", err, writes, serialReads, committed)
			}
		})
	}
}

func TestRoutingMetadataKeepsResolvingAfterSerialReadFailure(t *testing.T) {
	writes, resolutions, committed := 0, 0, false
	err := ensureRoutingMetadata(context.Background(), "model", func(_ context.Context, serial bool) error {
		if writes == 0 {
			return ErrNotFound
		}
		if !serial {
			t.Fatal("ordinary read used while a CAS result was still uncertain")
		}
		resolutions++
		if resolutions == 1 {
			return routingRequestError(gocql.ErrCodeReadTimeout)
		}
		committed = true
		return nil
	}, func(context.Context) error {
		writes++
		return routingRequestError(gocql.ErrCodeCASWriteUnknown)
	})
	if err != nil || writes != 1 || resolutions != 2 || !committed {
		t.Fatal(err, writes, resolutions, committed)
	}
}

func TestRoutingMetadataRetriesAbsentProposal(t *testing.T) {
	writes, serialReads, committed := 0, 0, false
	err := ensureRoutingMetadata(context.Background(), "route", func(_ context.Context, serial bool) error {
		if serial {
			serialReads++
		}
		if committed {
			return nil
		}
		return gocql.ErrNotFound
	}, func(context.Context) error {
		writes++
		if writes == 1 {
			return routingRequestError(gocql.ErrCodeCASWriteUnknown)
		}
		committed = true
		return nil
	})
	if err != nil || writes != 2 || serialReads < 1 || !committed {
		t.Fatal(err, writes, serialReads, committed)
	}
}

func TestRoutingMetadataExistingAndConflictingValues(t *testing.T) {
	conflict := errors.New("conflicting existing metadata")
	for _, readErr := range []error{nil, conflict, routingRequestError(gocql.ErrCodeUnauthorized)} {
		reads, writes := 0, 0
		err := ensureRoutingMetadata(context.Background(), "route", func(context.Context, bool) error {
			reads++
			return readErr
		}, func(context.Context) error { writes++; return nil })
		if !errors.Is(err, readErr) || reads != 1 || writes != 0 {
			t.Fatalf("existing metadata was rewritten/retried: %v reads=%d writes=%d", err, reads, writes)
		}
	}
	// A competing writer can win the insert. Its conflicting value must still
	// fail validation, including when our own write returned an unknown result.
	for _, writeErr := range []error{nil, routingRequestError(gocql.ErrCodeCASWriteUnknown)} {
		reads := 0
		err := ensureRoutingMetadata(context.Background(), "model", func(context.Context, bool) error {
			reads++
			if reads == 1 {
				return ErrNotFound
			}
			return conflict
		}, func(context.Context) error { return writeErr })
		if !errors.Is(err, conflict) || reads != 2 {
			t.Fatal("concurrent conflict accepted", err, reads)
		}
	}
}

func TestRoutingMetadataStopsOnPermanentWriteFailure(t *testing.T) {
	denied := routingRequestError(gocql.ErrCodeUnauthorized)
	reads, writes := 0, 0
	err := ensureRoutingMetadata(context.Background(), "route", func(context.Context, bool) error {
		reads++
		return ErrNotFound
	}, func(context.Context) error { writes++; return denied })
	if !errors.Is(err, denied) || reads != 1 || writes != 1 {
		t.Fatal(err, reads, writes)
	}
}

func TestRoutingMetadataPersistentQuorumFailureIsBounded(t *testing.T) {
	unavailable := routingRequestError(gocql.ErrCodeUnavailable)
	reads := 0
	err := ensureRoutingMetadata(context.Background(), "route (12,7)", func(context.Context, bool) error {
		reads++
		return unavailable
	}, func(context.Context) error { t.Fatal("write attempted without an acknowledged read"); return nil })
	if !errors.Is(err, unavailable) || reads != 5 || !strings.Contains(err.Error(), "route (12,7)") || !strings.Contains(err.Error(), "replica health") {
		t.Fatal(err, reads)
	}
}

func TestRoutingMetadataCancellationInterruptsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads := 0
	start := time.Now()
	err := ensureRoutingMetadata(ctx, "route", func(context.Context, bool) error {
		reads++
		cancel()
		return routingRequestError(gocql.ErrCodeUnavailable)
	}, func(context.Context) error { t.Fatal("write after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) || reads != 1 || time.Since(start) > time.Second {
		t.Fatal(err, reads)
	}
}

func TestRoutingSetupErrorClassification(t *testing.T) {
	for _, code := range []int{gocql.ErrCodeCASWriteUnknown, gocql.ErrCodeWriteTimeout, gocql.ErrCodeReadTimeout, gocql.ErrCodeUnavailable, gocql.ErrCodeOverloaded, gocql.ErrCodeBootstrapping, gocql.ErrCodeReadFailure, gocql.ErrCodeWriteFailure} {
		if !retryableRoutingError(fmt.Errorf("wrapped: %w", routingRequestError(code))) {
			t.Fatalf("transient Cassandra error %x cannot recover", code)
		}
	}
	for _, err := range []error{gocql.ErrTimeoutNoResponse, gocql.ErrConnectionClosed, gocql.ErrNoConnections} {
		if !retryableRoutingError(err) {
			t.Fatal(err)
		}
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, routingRequestError(gocql.ErrCodeInvalid), routingRequestError(gocql.ErrCodeUnauthorized), routingRequestError(gocql.ErrCodeSyntax), errors.New("conflicting metadata")} {
		if retryableRoutingError(err) {
			t.Fatal("permanent failure retried", err)
		}
	}
}
