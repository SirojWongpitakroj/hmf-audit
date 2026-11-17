package canonicalingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// WaitForProducerRoom reserves one *whole native epoch* of lead. Missing
// consumers count as zero; malformed/changed identities are fatal. No consumer
// is asked to catch up after every publication, and no cursor is updated here.
func WaitForProducerRoom(ctx context.Context, source Store, native *sql.DB, stream string, ids []string, nextFinalized, maxAhead uint64, poll time.Duration) error {
	if len(ids) != 3 || maxAhead == 0 {
		return errors.New("three required consumers and a positive producer lead required")
	}
	for {
		m, err := source.Manifest(ctx, stream)
		if err != nil {
			return err
		}
		slowest := m.Committed
		for _, id := range ids {
			var n uint64
			var consumerStream, fp string
			err = native.QueryRowContext(ctx, `SELECT committed,stream_id,fingerprint FROM canonical_ingestion.consumers WHERE consumer_id=$1`, id).Scan(&n, &consumerStream, &fp)
			if errors.Is(err, sql.ErrNoRows) {
				n = 0
			} else if err != nil {
				return err
			} else if consumerStream != stream || fp != m.Fingerprint || n > m.Committed {
				return fmt.Errorf("consumer %s conflicts with published stream", id)
			}
			slowest = min(slowest, n)
		}
		if nextFinalized <= slowest || nextFinalized-slowest <= maxAhead {
			return nil
		}
		if err = Wait(ctx, poll); err != nil {
			return err
		}
	}
}
