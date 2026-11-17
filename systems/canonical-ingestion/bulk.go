package canonicalingestion

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type SQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// InsertRows uses bound parameters only. Prefix/suffix are trusted SQL literals
// owned by adapters. It preserves the caller's transaction and constraints; a
// conflict is never turned into an overwrite. Each statement is byte/row bounded.
func InsertRows(ctx context.Context, q SQLExecutor, prefix, suffix string, rows [][]any, maxRecords, maxBytes int, m *IngestMetrics) error {
	if maxRecords < 1 || maxBytes < 1 {
		return fmt.Errorf("invalid SQL batch limits")
	}
	for start := 0; start < len(rows); {
		cols := len(rows[start])
		if cols == 0 || cols > 65535 {
			return fmt.Errorf("invalid SQL row width")
		}
		end, bytes := start, 0
		var args []any
		var b strings.Builder
		b.WriteString(prefix)
		for end < len(rows) && end-start < min(maxRecords, 65535/cols) {
			row := rows[end]
			if len(row) != cols {
				return fmt.Errorf("inconsistent SQL row width")
			}
			cost := cols * 32
			for _, v := range row {
				switch v := v.(type) {
				case []byte:
					cost += len(v)
				case string:
					cost += len(v)
				default:
					cost += 16
				}
			}
			if cost > maxBytes {
				return fmt.Errorf("SQL row exceeds batch byte limit")
			}
			if bytes > maxBytes-cost {
				break
			}
			if end > start {
				b.WriteByte(',')
			}
			b.WriteByte('(')
			for i := range row {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteByte('$')
				b.WriteString(strconv.Itoa(len(args) + i + 1))
			}
			b.WriteByte(')')
			args = append(args, row...)
			bytes += cost
			end++
		}
		b.WriteString(suffix)
		began := time.Now()
		_, err := q.ExecContext(ctx, b.String(), args...)
		if m != nil {
			m.ObserveSQL(time.Since(began), err)
			m.SQLBatches.Add(1)
		}
		if err != nil {
			return err
		}
		start = end
	}
	return nil
}
