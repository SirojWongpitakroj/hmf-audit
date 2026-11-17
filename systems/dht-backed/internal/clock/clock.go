// Package clock provides the injectable Clock seam required by Q40.
//
// Normal execution uses the system UTC wall clock. Tests inject a Manual clock
// so token expiry is exercised deterministically, never by sleeping.
package clock

import "time"

// Clock supplies the current instant.
type Clock interface {
	// Now returns the current time in UTC.
	Now() time.Time
}

// System is the system UTC wall clock used in normal execution (Q40).
type System struct{}

// Now returns the current system time in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Manual is a Clock whose value is set explicitly. It is a testability seam
// (Q40), not an optimization.
type Manual struct {
	T time.Time
}

// Now returns the manually set instant in UTC.
func (m *Manual) Now() time.Time { return m.T.UTC() }

// Set replaces the manual clock's instant.
func (m *Manual) Set(t time.Time) { m.T = t }

// Advance moves the manual clock forward by d.
func (m *Manual) Advance(d time.Duration) { m.T = m.T.Add(d) }
