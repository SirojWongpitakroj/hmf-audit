package query

import (
	"fmt"

	"blas/alis"
)

// SP is the off-chain storage service provider (R-FUNC-013): untrusted
// (R-SEC-001) — every response it produces is re-verified by the auditor,
// never taken on faith. It stores full entry-level files and answers Q_off
// for the fids the auditor selects.
type SP struct {
	files map[FileRef]*alis.EntryFile
}

// StorageProvider is the request-time interface used to generate Q_off
// evidence. The in-process test SP and the PostgreSQL-backed provider both
// implement it.
type StorageProvider interface {
	AnswerQOff(refs []FileRef, qoff QOff) ([]FileResult, error)
}

// NewSP returns an empty SP.
func NewSP() *SP { return &SP{files: map[FileRef]*alis.EntryFile{}} }

// Store adds one closed entry-level file to the SP (R-FUNC-004: the log
// generator sends raw log data — modeled here as the already-built
// EntryFile — to the SP).
func (sp *SP) Store(ref FileRef, f *alis.EntryFile) { sp.files[ref] = f }

// ForEachFile visits every stored file during persistence setup. Iteration
// order is unspecified and must not feed authenticated state.
func (sp *SP) ForEachFile(visit func(FileRef, *alis.EntryFile) error) error {
	for ref, file := range sp.files {
		if err := visit(ref, file); err != nil {
			return err
		}
	}
	return nil
}

// FileResult is one file's Q_off answer (R-QUERY-021..024): the entry-level
// VO (which itself carries the returned entries via its OMT proofs).
type FileResult struct {
	Ref FileRef
	VO  alis.EntryVO
}

// AnswerQOff implements R-QUERY-021..024 for the fids the auditor selected
// (G1 in DECISIONS.md: the auditor sends the selected fids plus the
// already-known lg and Q_off — text over the Fig. 4 figure label). A
// missing file is a hard error (G1: "a missing required SP response ⇒
// reject: the audit result is incomplete" — the caller/verify layer turns
// this into a reject, per R-GO-025's single accept/reject discipline).
func (sp *SP) AnswerQOff(refs []FileRef, qoff QOff) ([]FileResult, error) {
	var out []FileResult
	for _, ref := range refs {
		f, ok := sp.files[ref]
		if !ok {
			return nil, fmt.Errorf("query: SP has no file for %+v", ref)
		}
		vo, err := f.ProcessQuery(alis.EntryQuery{EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE})
		if err != nil {
			return nil, err
		}
		out = append(out, FileResult{Ref: ref, VO: vo})
	}
	return out, nil
}

// File returns the stored EntryFile for ref, if any — used by the auditor
// only to look up the ROOTS it needs from the (already Stage-2-verified)
// transaction, never to bypass verification.
func (sp *SP) File(ref FileRef) (*alis.EntryFile, bool) {
	f, ok := sp.files[ref]
	return f, ok
}
