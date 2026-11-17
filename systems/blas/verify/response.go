package verify

import (
	"blas/alis"
	"blas/iomf"
	"blas/query"
)

// Response is the untrusted evidence returned by the full node and storage
// provider. Keeping it separate from its producers lets benchmarks time the
// auditor's verification without also timing proof/query generation.
type Response struct {
	Stage1   query.Stage1Result
	Stage2   query.Stage2Result
	Bindings map[uint64]query.AggregatedLGBinding
	Files    []query.FileResult
}

// PrepareResponse builds a complete response outside the core verifier timer.
// The response remains untrusted: VerifyResponse rechecks every stage.
func (a *Auditor) PrepareResponse(fn *query.FullNode, sp query.StorageProvider,
	keywordUniverse map[uint64][]string, q query.Q, opts Options) (Response, bool) {
	qon, _ := query.Split(q)
	var response Response
	var err error
	if opts.UseSegmentScan {
		response.Stage1, err = fn.Stage1Optimized(qon)
	} else {
		response.Stage1, err = fn.Stage1(qon)
	}
	if err != nil {
		return Response{}, false
	}
	if opts.UseAggregation {
		response.Stage2, response.Bindings, err = fn.Stage2Aggregated(qon, response.Stage1)
	} else {
		response.Stage2, err = fn.Stage2(qon, response.Stage1.CandidateHeights)
	}
	if err != nil {
		return Response{}, false
	}
	return a.CompleteResponse(sp, keywordUniverse, q, response, opts)
}

// CompleteResponse adds the storage-provider evidence to an already generated
// Stage 1/2 response. Its preliminary checks are only used to determine the
// requested files; VerifyResponse repeats all checks inside the timed core.
func (a *Auditor) CompleteResponse(sp query.StorageProvider, keywordUniverse map[uint64][]string,
	q query.Q, response Response, opts Options) (Response, bool) {
	_, qoff := query.Split(q)
	refs, ok := a.RequestedFiles(keywordUniverse, q, response, opts)
	if !ok {
		return Response{}, false
	}
	files, err := sp.AnswerQOff(refs, qoff)
	if err != nil {
		return Response{}, false
	}
	response.Files = files
	return response, true
}

// RequestedFiles performs the preliminary auditor pass needed by the online
// protocol to choose Q_off file references. It is a preparation helper, not a
// substitute for VerifyResponse, which repeats every validation.
func (a *Auditor) RequestedFiles(keywordUniverse map[uint64][]string,
	q query.Q, response Response, opts Options) ([]query.FileRef, bool) {
	transactions, ok := a.verifyResponseOnChain(q, response, opts)
	if !ok {
		return nil, false
	}
	_, qoff := query.Split(q)
	_, refs, ok := selectedFiles(transactions, qoff.EK, keywordUniverse)
	return refs, ok
}

// VerifyResponse is the pure auditor-side benchmark boundary. It performs all
// proof validation and result-completeness checks, but no full-node or storage
// provider calls.
func (a *Auditor) VerifyResponse(keywordUniverse map[uint64][]string,
	q query.Q, response Response, opts Options) Result {
	transactions, ok := a.verifyResponseOnChain(q, response, opts)
	if !ok {
		return Result{Accept: false}
	}
	_, qoff := query.Split(q)
	expected, refs, ok := selectedFiles(transactions, qoff.EK, keywordUniverse)
	if !ok || len(response.Files) != len(refs) {
		return Result{Accept: false}
	}
	return verifyFileEvidence(expected, qoff, response.Files)
}

func (a *Auditor) verifyResponseOnChain(q query.Q, response Response, opts Options) ([]alis.Transaction, bool) {
	qon, _ := query.Split(q)
	var candidates []uint64
	var ok bool
	if opts.UseSegmentScan {
		candidates, ok = a.verifyStage1Optimized(qon, response.Stage1)
	} else {
		candidates, ok = a.verifyStage1(qon, response.Stage1)
	}
	if !ok {
		return nil, false
	}
	if opts.UseAggregation {
		return a.verifyStage2Aggregated(qon, candidates, response.Stage2, response.Bindings)
	}
	return a.verifyStage2(qon, candidates, response.Stage2)
}

func selectedFiles(transactions []alis.Transaction, expression *iomf.Expr,
	keywordUniverse map[uint64][]string) (map[query.FileRef]alis.Transaction, []query.FileRef, bool) {
	selected := filterByBmEW(transactions, expression, keywordUniverse)
	byRef := make(map[query.FileRef]alis.Transaction, len(selected))
	refs := make([]query.FileRef, 0, len(selected))
	for _, transaction := range selected {
		ref := query.FileRef{LID: transaction.LID, FID: transaction.FID}
		if _, found := byRef[ref]; found {
			return nil, nil, false
		}
		byRef[ref] = transaction
		refs = append(refs, ref)
	}
	return byRef, refs, true
}

func verifyFileEvidence(expected map[query.FileRef]alis.Transaction,
	qoff query.QOff, files []query.FileResult) Result {
	seen := make(map[query.FileRef]struct{}, len(files))
	var entries []alis.LogEntry
	for _, file := range files {
		transaction, found := expected[file.Ref]
		if !found {
			return Result{Accept: false}
		}
		if _, duplicate := seen[file.Ref]; duplicate {
			return Result{Accept: false}
		}
		seen[file.Ref] = struct{}{}
		entryQuery := alis.EntryQuery{
			EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE,
		}
		if !alis.VerifyEntryQuery(transaction.RHIIMT, transaction.RHOMT, entryQuery, file.VO) {
			return Result{Accept: false}
		}
		for _, proof := range file.VO.ResultProofs {
			entry, err := alis.DecodeLogEntry(proof.Object)
			if err != nil {
				return Result{Accept: false}
			}
			entries = append(entries, entry)
		}
	}
	if len(seen) != len(expected) {
		return Result{Accept: false}
	}
	return Result{Entries: entries, Accept: true}
}
