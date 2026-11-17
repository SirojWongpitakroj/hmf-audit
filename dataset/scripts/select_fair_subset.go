// Command select_fair_subset creates the exact shared input used by the fair
// cross-system benchmark. It selects evenly spaced records independently from
// each region, then writes the regions in R0..R7 order. With -all it keeps
// every record, still grouped by region in source order.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const scannerBuffer = 4 << 20

type combinedRecord struct {
	SourceID string `json:"source_id"`
	RawLog   string `json:"raw_log"`
	RegionID string `json:"region_id"`
	TenantID string `json:"tenant_id"`
}

func main() {
	input := flag.String("input", "", "combined JSONL input")
	output := flag.String("output", "", "selected JSONL output")
	regions := flag.Int("regions", 8, "number of regions R0..R(n-1)")
	perRegion := flag.Int("per-region", 4096, "records selected per region")
	all := flag.Bool("all", false, "select every record of each region (overrides -per-region)")
	flag.Parse()
	if *input == "" || *output == "" || *regions <= 0 || (!*all && *perRegion <= 0) {
		fatalf("input, output, regions, and per-region are required and must be positive")
	}

	counts := make([]int, *regions)
	total := 0
	if err := scan(*input, func(_ []byte, record combinedRecord) error {
		region, err := regionIndex(record.RegionID, *regions)
		if err != nil {
			return err
		}
		counts[region]++
		total++
		return nil
	}); err != nil {
		fatalf("count input: %v", err)
	}
	// With every record wanted, the evenly spaced rule below selects each
	// position k = k*(M-1)/(M-1) exactly once.
	want := make([]int, *regions)
	for region, count := range counts {
		want[region] = *perRegion
		if *all {
			want[region] = count
		}
		if want[region] <= 0 || count < want[region] {
			fatalf("region R%d has %d records, need %d", region, count, want[region])
		}
	}

	// Selected lines are spilled to one temporary file per region, so memory
	// stays constant however many records are selected; the files are then
	// concatenated in R0..R7 order.
	parts := make([]*os.File, *regions)
	writers := make([]*bufio.Writer, *regions)
	removeParts := func() {
		for _, part := range parts {
			if part != nil {
				_ = part.Close()
				_ = os.Remove(part.Name())
			}
		}
	}
	for region := range *regions {
		part, err := os.Create(fmt.Sprintf("%s.R%d.part", *output, region))
		if err != nil {
			removeParts()
			fatalf("create region part: %v", err)
		}
		parts[region] = part
		writers[region] = bufio.NewWriterSize(part, 1<<20)
	}

	seen := make([]int, *regions)
	selected := make([]int, *regions)
	if err := scan(*input, func(line []byte, record combinedRecord) error {
		region, err := regionIndex(record.RegionID, *regions)
		if err != nil {
			return err
		}
		position := seen[region]
		seen[region]++
		if selected[region] >= want[region] {
			return nil
		}
		target := 0
		if want[region] > 1 {
			target = selected[region] * (counts[region] - 1) / (want[region] - 1)
		}
		if position != target {
			return nil
		}
		if record.SourceID == "" || record.RawLog == "" || record.TenantID == "" {
			return fmt.Errorf("selected record in %s has an empty required field", record.RegionID)
		}
		if _, err := writers[region].Write(line); err != nil {
			return err
		}
		if err := writers[region].WriteByte('\n'); err != nil {
			return err
		}
		selected[region]++
		return nil
	}); err != nil {
		removeParts()
		fatalf("select input: %v", err)
	}
	for region := range *regions {
		if selected[region] != want[region] {
			removeParts()
			fatalf("selected %d records for R%d, want %d", selected[region], region, want[region])
		}
		if err := writers[region].Flush(); err != nil {
			removeParts()
			fatalf("flush region part: %v", err)
		}
	}

	file, err := os.Create(*output)
	if err != nil {
		removeParts()
		fatalf("create output: %v", err)
	}
	for _, part := range parts {
		if _, err := part.Seek(0, io.SeekStart); err != nil {
			_ = file.Close()
			removeParts()
			fatalf("rewind region part: %v", err)
		}
		if _, err := io.Copy(file, part); err != nil {
			_ = file.Close()
			removeParts()
			fatalf("write output: %v", err)
		}
	}
	removeParts()
	if err := file.Close(); err != nil {
		fatalf("close output: %v", err)
	}
	selectedTotal := 0
	for _, count := range want {
		selectedTotal += count
	}
	if *all {
		fmt.Printf("selected %d of %d records (all records, per region %v)\n", selectedTotal, total, want)
	} else {
		fmt.Printf("selected %d of %d records (%d per region)\n", selectedTotal, total, *perRegion)
	}
}

func scan(path string, visit func([]byte, combinedRecord) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), scannerBuffer)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var record combinedRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if err := visit(line, record); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func regionIndex(regionID string, regions int) (int, error) {
	if !strings.HasPrefix(regionID, "R") {
		return 0, fmt.Errorf("invalid region_id %q", regionID)
	}
	index, err := strconv.Atoi(strings.TrimPrefix(regionID, "R"))
	if err != nil || index < 0 || index >= regions {
		return 0, fmt.Errorf("invalid region_id %q", regionID)
	}
	return index, nil
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "select_fair_subset: "+format+"\n", values...)
	os.Exit(1)
}
