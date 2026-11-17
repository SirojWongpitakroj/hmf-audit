package orchestration

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func progressEncoder(out io.Writer, format string) (func(Progress) error, error) {
	switch format {
	case "json":
		encoder := json.NewEncoder(out)
		return func(p Progress) error { return encoder.Encode(p) }, nil
	case "text":
		return func(p Progress) error {
			status := "RUNNING"
			if p.Final {
				status = "STOPPED"
				if p.Complete() {
					status = "COMPLETE"
				}
			}
			if p.Phase != "" {
				status += " phase=" + p.Phase
			}
			mva := "waiting for telemetry"
			if (p.MVAFinalizeFirst || p.MVAPublishEachEpoch) && p.Phase == "baselines" {
				mva = "finalized; baselines consuming canonical prefix"
			}
			if m, ok := p.Children["mva"]; ok && ((!p.MVAFinalizeFirst && !p.MVAPublishEachEpoch) || p.Phase != "baselines") {
				// This is activity within this child process, not a durable cursor.
				// Keeping it session-relative avoids inventing progress on replay.
				mva = fmt.Sprintf("%s processed this session (~%.0f rec/s)", count(m.StagedRecords), m.RollingAcceptedRecordsPerSecond)
			}
			rss := "collecting"
			var bytes uint64
			all := true
			names := []string{"mva", "delia", "blas", "dht"}
			if p.MVAFinalizeFirst || p.MVAPublishEachEpoch {
				names = nil
				for name := range p.Children {
					names = append(names, name)
				}
				all = len(names) > 0
			}
			for _, name := range names {
				child, ok := p.Children[name]
				if !ok || !child.Memory.RSSAvailable {
					all = false
					break
				}
				bytes += child.Memory.RSSBytes
			}
			if all {
				rss = fmt.Sprintf("%.0f MiB", float64(bytes)/(1<<20))
			}
			_, err := fmt.Fprintf(out,
				"[%s] %s | Target %s | MVA: %s\n  Finalized %s | Published %s | DELIA %s (%.0f rec/s) | BLAS %s (%.0f rec/s) | DHT accepted %s (%.0f rec/s), proofs %s | Workers RSS %s\n",
				p.Time.Local().Format("15:04:05"), status, count(p.Target), mva,
				count(p.MVAFinalized), count(p.Published), count(p.DELIA.Committed), p.DELIA.Rate, count(p.BLAS.Committed), p.BLAS.Rate, count(p.DHTAccepted), p.DHT.Rate, count(p.DHTProofComplete), rss)
			return err
		}, nil
	default:
		return nil, fmt.Errorf("invalid progress format %q: use text or json", format)
	}
}

func count(n uint64) string {
	s := strconv.FormatUint(n, 10)
	var b strings.Builder
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
