package canonicalingestion

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func ReadAuditRequest(path string, limits Limits) (AuditRequest, error) {
	var request AuditRequest
	if err := limits.Validate(); err != nil {
		return request, err
	}
	f, err := os.Open(path)
	if err != nil {
		return request, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(limits.MaxBatchBytes)*2+1))
	if err != nil {
		return request, err
	}
	if len(b) > limits.MaxBatchBytes*2 {
		return request, errors.New("audit request file exceeds byte limit")
	}
	if err = json.Unmarshal(b, &request); err != nil {
		return request, err
	}
	return request, request.Validate(limits)
}
func WriteAuditReport(path string, report AuditReport) error {
	if path == "" {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create audit report (existing files are preserved): %w", err)
	}
	err = json.NewEncoder(f).Encode(report)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
