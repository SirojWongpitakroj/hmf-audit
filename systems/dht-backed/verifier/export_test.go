package verifier

// Test-only aliases for the internal rejection reasons.
//
// The GAP-4 decision keeps these reasons internal: the verifier's public result
// is accept/reject only, and every reason wraps the exported ErrRejected. This
// file is compiled only by `go test` for this package, so the verifier's own
// tests can still confirm WHICH recomputation caught a tamper without any of
// these names being part of the API.
var (
	ErrRecordNotFound   = errRecordNotFound
	ErrProofUnavailable = errProofUnavailable
	ErrProofMalformed   = errProofMalformed
	ErrPathMismatch     = errPathMismatch
	ErrAnchorMismatch   = errAnchorMismatch
	ErrNotCovered       = errNotCovered
)
