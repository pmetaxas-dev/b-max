// Package clock is the single source of "now" for the server. Every service
// takes a Clock so the App Clock (Phase 5) can replace it without touching rules.
package clock

import "time"

type Clock interface {
	Now() time.Time
}

// Real is wall-clock time in the server's local zone. A "day" is local midnight.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }
