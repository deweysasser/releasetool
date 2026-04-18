// Package timing provides a small Timer helper for measuring and logging
// the duration of interesting operations. Output is written to zerolog's
// global Logger at Debug level — enable debug logging to see it.
package timing

import (
	"time"

	"github.com/rs/zerolog/log"
)

// Timer captures the start time of a named operation and, when Done is
// called, emits a single zerolog debug event carrying the operation name,
// start time, end time, and elapsed duration.
//
// Designed for use with defer:
//
//	func FetchReleases() {
//	    defer timing.Start("FetchReleases").Done()
//	    // ...
//	}
//
// All methods are safe to call from any goroutine.
type Timer struct {
	name  string
	start time.Time
}

// Start records the current wall-clock time as the start of a named
// operation and returns a *Timer whose Done method emits the timing event.
func Start(name string) *Timer {
	return &Timer{name: name, start: time.Now()}
}

// Done emits a single debug-level log event containing the operation name
// plus start, end, and elapsed times. Safe to call from defer; safe to call
// from any goroutine.
func (t *Timer) Done() {
	end := time.Now()
	log.Debug().
		Str("op", t.name).
		Time("start", t.start).
		Time("end", end).
		Dur("elapsed", end.Sub(t.start)).
		Msg("timing")
}
