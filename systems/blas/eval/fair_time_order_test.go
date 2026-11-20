package eval

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

// fairBLASRecords is the dataset as the BLAS builder reads it, one JSON line at a time.
// *bufio.Scanner is the original source (file order); fairBLASTimeOrderedRecords is the
// event-time order used by the attribute fixture.
type fairBLASRecords interface {
	Scan() bool
	Bytes() []byte
	Err() error
}

// fairBLASRecordsOf returns the records in the order the active fixture ingests them. With the
// attribute fixture (the default) that is event-time order within each region, so that a BLAS time
// range is a range of the same event time an HMF-Audit range query uses; with FAIR_BLAS_ATTRIBUTES=0 it
// is the file order and the ordinal timestamps of the original fixture.
func fairBLASRecordsOf(file *os.File, total int) (fairBLASRecords, error) {
	if !fairBLASAttributesEnabled() {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 4<<20)
		return scanner, nil
	}
	return newFairBLASTimeOrderedRecords(file, total)
}

// fairBLASRecordTime is the timestamp BLAS stamps a log with: the dataset's event time in
// milliseconds for the attribute fixture, else the original fixture's timestamp.
func fairBLASRecordTime(input fairBLASDatasetRecord, ordinal int) (int64, error) {
	if !fairBLASAttributesEnabled() {
		return fairBLASTimestamp(input.RawLog, ordinal), nil
	}
	when, err := time.Parse(time.RFC3339, input.EventTime)
	if err != nil {
		return 0, fmt.Errorf("record %d has no usable event_time (%q): %w", ordinal, input.EventTime, err)
	}
	return when.UnixMilli(), nil
}

// fairBLASTimeOrderedRecords serves the records of the dataset sorted by (region, event time, file
// position). A first pass records where every line starts and how long it is, its region and its event
// time; the lines are then read back in sorted order. Only about 20 bytes per record are held.
type fairBLASTimeOrderedRecords struct {
	file    *os.File
	offsets []int64
	lengths []int32
	order   []int32
	next    int
	buffer  []byte
	err     error
}

var (
	fairBLASRegionMarker = []byte(`"region_id":"R`)
	fairBLASTimeMarker   = []byte(`"event_time":"`)
)

func newFairBLASTimeOrderedRecords(file *os.File, total int) (*fairBLASTimeOrderedRecords, error) {
	offsets := make([]int64, 0, total)
	lengths := make([]int32, 0, total)
	times := make([]int64, 0, total)
	regions := make([]uint8, 0, total)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var consumed, lineStart int64
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		start := consumed
		advance, token, err := bufio.ScanLines(data, atEOF)
		consumed += int64(advance)
		if token != nil {
			lineStart = start
		}
		return advance, token, err
	})
	for scanner.Scan() {
		line := scanner.Bytes()
		position := len(offsets)
		region, ok := fairBLASFieldDigits(line, fairBLASRegionMarker)
		if !ok || region > 255 {
			return nil, fmt.Errorf("record %d has no region_id of the form R<number>", position)
		}
		start := bytes.Index(line, fairBLASTimeMarker)
		if start < 0 {
			return nil, fmt.Errorf("record %d has no event_time", position)
		}
		start += len(fairBLASTimeMarker)
		end := bytes.IndexByte(line[start:], '"')
		if end < 0 {
			return nil, fmt.Errorf("record %d has an unterminated event_time", position)
		}
		when, err := time.Parse(time.RFC3339, string(line[start:start+end]))
		if err != nil {
			return nil, fmt.Errorf("record %d event_time: %w", position, err)
		}
		offsets = append(offsets, lineStart)
		lengths = append(lengths, int32(len(line)))
		times = append(times, when.Unix())
		regions = append(regions, uint8(region))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(offsets) != total {
		return nil, fmt.Errorf("dataset has %d records, expected %d", len(offsets), total)
	}
	order := make([]int32, total)
	for index := range order {
		order[index] = int32(index)
	}
	slices.SortFunc(order, func(a, b int32) int {
		switch {
		case regions[a] != regions[b]:
			return int(regions[a]) - int(regions[b])
		case times[a] != times[b]:
			if times[a] < times[b] {
				return -1
			}
			return 1
		}
		return int(a) - int(b)
	})
	return &fairBLASTimeOrderedRecords{file: file, offsets: offsets, lengths: lengths, order: order}, nil
}

// fairBLASFieldDigits reads the decimal number that follows marker in line.
func fairBLASFieldDigits(line, marker []byte) (int, bool) {
	start := bytes.Index(line, marker)
	if start < 0 {
		return 0, false
	}
	value, digits := 0, 0
	for _, char := range line[start+len(marker):] {
		if char < '0' || char > '9' {
			break
		}
		value = value*10 + int(char-'0')
		digits++
	}
	return value, digits > 0
}

func (records *fairBLASTimeOrderedRecords) Scan() bool {
	if records.err != nil || records.next >= len(records.order) {
		return false
	}
	index := records.order[records.next]
	records.next++
	length := int(records.lengths[index])
	if cap(records.buffer) < length {
		records.buffer = make([]byte, length)
	}
	records.buffer = records.buffer[:length]
	read, err := records.file.ReadAt(records.buffer, records.offsets[index])
	if read < length {
		if err == nil || err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		records.err = err
		return false
	}
	return true
}

func (records *fairBLASTimeOrderedRecords) Bytes() []byte { return records.buffer }

func (records *fairBLASTimeOrderedRecords) Err() error { return records.err }
