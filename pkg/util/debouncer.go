package util

import (
	"sync"
	"time"
)

// Debouncer resets a timer whenever Reset is called, useful for timeout-based operations.
// It's thread-safe and handles all timer edge cases properly.
//
// Example usage:
//
//	debouncer := NewDebouncer(200 * time.Millisecond)
//	defer debouncer.Stop()
//
//	for {
//	    select {
//	    case data := <-dataChannel:
//	        processData(data)
//	        debouncer.Reset() // Reset timeout on each data received
//	    case <-debouncer.C():
//	        commitData() // Timeout reached, commit accumulated data
//	    }
//	}
type Debouncer struct {
	duration time.Duration
	timer    *time.Timer
	mu       sync.Mutex
	stopped  bool
}

// NewDebouncer creates a new debouncer with the specified duration
func NewDebouncer(duration time.Duration) *Debouncer {
	return &Debouncer{
		duration: duration,
		timer:    time.NewTimer(duration),
	}
}

// Reset resets the timer to fire after the debouncer's duration.
// If the debouncer has been stopped, this is a no-op.
func (d *Debouncer) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	// Stop timer and drain channel if necessary
	if !d.timer.Stop() {
		select {
		case <-d.timer.C:
		default:
		}
	}
	d.timer.Reset(d.duration)
}

// C returns the timer's channel
func (d *Debouncer) C() <-chan time.Time {
	return d.timer.C
}

// Stop stops the debouncer and prevents further resets.
// It's safe to call Stop multiple times.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.stopped {
		d.timer.Stop()
		d.stopped = true
	}
}