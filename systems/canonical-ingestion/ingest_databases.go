package canonicalingestion

import (
	"context"
	"database/sql"
)

// OpenIngestionDatabases shares the pool when the canonical stream uses the
// baseline database (the normal deployment), rather than doubling its limit.
func OpenIngestionDatabases(ctx context.Context, poolSize int) (source, native *sql.DB, close func(), err error) {
	sourceDSN, err := PostgresDSN("CANONICAL_STREAM", "BASELINES_POSTGRES")
	if err != nil {
		return nil, nil, nil, err
	}
	nativeDSN, err := PostgresDSN("BASELINES_POSTGRES", "")
	if err != nil {
		return nil, nil, nil, err
	}
	source, err = OpenPostgres(ctx, sourceDSN, poolSize)
	if err != nil {
		return nil, nil, nil, err
	}
	if sourceDSN == nativeDSN {
		return source, source, func() { source.Close() }, nil
	}
	native, err = OpenPostgres(ctx, nativeDSN, poolSize)
	if err != nil {
		source.Close()
		return nil, nil, nil, err
	}
	return source, native, func() { native.Close(); source.Close() }, nil
}
