package eval

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// androidV1LineRE matches the LogHub Android_v1 line format:
//
//	MM-DD HH:MM:SS.mmm  PID  TID LEVEL Component: message
//
// e.g. "12-17 19:31:36.263  1795  1825 I PowerManager_screenOn: ...".
// Lines that don't match this shape (continuation lines, stack traces,
// truncated records — real, "not sanitized" log data, per loghub's own
// README) are skipped rather than erroring: a parser that aborted on the
// first malformed line would not survive contact with a real dataset.
var androidV1LineRE = regexp.MustCompile(
	`^(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})\.(\d{3})\s+\d+\s+\d+\s+(\S)\s+([^:]+):`)

// androidV1ReferenceYear is the fixed year assigned to every record that
// has no year in the raw log (H4 in DECISIONS.md: "fill an absent year
// deterministically"). The paper/rulings do not say WHICH year — this is
// the harness's own one-line, documented deterministic choice (see
// testdata/README.md), not a claim about the dataset's true collection
// date. A month that rolls backward (December → January) partway through
// the file increments the year, so records stay in a single, internally
// consistent, monotonically-plausible timeline.
const androidV1ReferenceYear = 2017

// LoadAndroidV1 parses a LogHub Android_v1-format log file into Records
// (H1/H4/H5): timestamps in Unix milliseconds with a deterministically
// filled year, and keywords extracted from the log LEVEL and Component
// name — literal tokens present in the line, standing in for LogHub's
// structured template/literal tokens where a full log-parsing/template
// -mining pipeline is out of scope for this reproduction (H5: "Record as a
// reproduction assumption"). Records are returned in file order (the
// generator-assignment step, AssignToGenerators, does the H1-mandated
// stable sort by timestamp afterward, not this loader).
func LoadAndroidV1(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []Record
	year := androidV1ReferenceYear
	prevMonth := -1

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		m := androidV1LineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		hour, _ := strconv.Atoi(m[3])
		minute, _ := strconv.Atoi(m[4])
		sec, _ := strconv.Atoi(m[5])
		ms, _ := strconv.Atoi(m[6])
		level := m[7]
		component := strings.TrimSpace(m[8])

		if prevMonth != -1 && month < prevMonth {
			year++
		}
		prevMonth = month

		t := time.Date(year, time.Month(month), day, hour, minute, sec, ms*1_000_000, time.UTC)
		records = append(records, Record{
			Timestamp: t.UnixMilli(),
			Keywords:  []string{"level:" + level, "component:" + component},
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("eval: no parseable records found in %s", path)
	}
	return records, nil
}
