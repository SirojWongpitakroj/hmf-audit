package auditor

import (
	"delia/globalstate"
	"delia/hash"
)

// State is one retrieved GlobalState in the {G_1..G_nu} sequence Alg. 3
// consumes. It is exactly globalstate.GlobalState; the alias keeps this
// package's exported surface self-contained per its own doc.
type State = globalstate.GlobalState

// stateEntryAt returns s.Entries[index], or the GAP-06 zero sentinel if
// index is out of range (defensive default; every Tier-A caller supplies a
// valid index within the frozen P order).
func stateEntryAt(s State, index int) hash.Digest {
	if index < 0 || index >= len(s.Entries) {
		return hash.Zero
	}
	return s.Entries[index]
}
