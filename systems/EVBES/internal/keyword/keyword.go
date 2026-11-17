// Package keyword is Phase 3 Step 1: Preprocess (Eq. 11), Tokenize (Eq. 12)
// and keyword selection K(LType) (Eq. 13).
//
// The paper gives no stopword list and no selection rule (A18). The list and
// the rule here are the B15 prototype choice: length >= MinLength (default 3)
// and alphabetic only. Both are configurable.
package keyword

import (
	"slices"
	"strings"
	"unicode"
)

// DefaultMinLength is B15's length threshold.
const DefaultMinLength = 3

// Stopwords is the fixed English stopword list removed by Preprocess (B15).
var Stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "but": true, "by": true, "for": true, "from": true, "has": true,
	"have": true, "he": true, "in": true, "is": true, "it": true, "its": true,
	"of": true, "on": true, "or": true, "she": true, "that": true, "the": true,
	"their": true, "then": true, "there": true, "these": true, "they": true,
	"this": true, "to": true, "was": true, "were": true, "will": true,
	"with": true, "not": true, "no": true, "if": true, "into": true, "such": true,
}

// Preprocess is Eq. 11: lower-case Ci, replace every character that is not a
// letter, digit or whitespace by a space (so "src=10.0.0.1" becomes
// "src 10 0 0 1"), and drop stopwords. Words are re-joined by single spaces.
func Preprocess(ci string) string {
	cleaned := strings.Map(func(r rune) rune {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, ci)
	words := strings.Fields(cleaned)
	kept := words[:0]
	for _, w := range words {
		if !Stopwords[w] {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

// Tokenize is Eq. 12: split C'i into the token set Tc = {t1, ..., tn}.
// Duplicates are removed; order of first occurrence is kept.
func Tokenize(c string) []string {
	var tc []string
	seen := map[string]bool{}
	for _, t := range strings.Fields(c) {
		if !seen[t] {
			seen[t] = true
			tc = append(tc, t)
		}
	}
	return tc
}

// Select is Eq. 13: K(LType) = {tj in Tc | tj satisfies selection criteria},
// with the B15 criteria (length >= minLength, letters only). The paper writes
// K as a function of LType but gives no per-type criteria, so LType is not an
// input.
func Select(tc []string, minLength int) []string {
	var k []string
	for _, t := range tc {
		if len([]rune(t)) >= minLength && !slices.ContainsFunc([]rune(t), func(r rune) bool { return !unicode.IsLetter(r) }) {
			k = append(k, t)
		}
	}
	return k
}

// Extract runs Eq. 11-13 with the default threshold.
func Extract(ci string) []string {
	return Select(Tokenize(Preprocess(ci)), DefaultMinLength)
}
