package util

import (
	"testing"
	"time"
)

func TestDebouncer(t *testing.T) {
	t.Run("fires after timeout", func(t *testing.T) {
		d := NewDebouncer(50 * time.Millisecond)
		defer d.Stop()

		select {
		case <-d.C():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Fatal("debouncer did not fire within expected time")
		}
	})

	t.Run("reset prevents firing", func(t *testing.T) {
		d := NewDebouncer(50 * time.Millisecond)
		defer d.Stop()

		// Reset every 25ms for 100ms
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			for i := 0; i < 4; i++ {
				<-ticker.C
				d.Reset()
			}
			close(done)
		}()

		// Should not fire during resets
		select {
		case <-d.C():
			t.Fatal("debouncer fired while being reset")
		case <-done:
			// Expected - resets prevented firing
		}

		// Should fire after resets stop
		select {
		case <-d.C():
			// Expected
		case <-time.After(100 * time.Millisecond):
			t.Fatal("debouncer did not fire after resets stopped")
		}
	})

	t.Run("stop prevents firing", func(t *testing.T) {
		d := NewDebouncer(50 * time.Millisecond)
		
		// Stop immediately
		d.Stop()

		select {
		case <-d.C():
			t.Fatal("debouncer fired after stop")
		case <-time.After(100 * time.Millisecond):
			// Expected - stop prevented firing
		}
	})

	t.Run("reset after stop is no-op", func(t *testing.T) {
		d := NewDebouncer(50 * time.Millisecond)
		d.Stop()
		
		// Should not panic
		d.Reset()
		
		// Should not fire
		select {
		case <-d.C():
			t.Fatal("debouncer fired after stop and reset")
		case <-time.After(100 * time.Millisecond):
			// Expected
		}
	})

	t.Run("multiple stops are safe", func(t *testing.T) {
		d := NewDebouncer(50 * time.Millisecond)
		
		// Should not panic
		d.Stop()
		d.Stop()
		d.Stop()
	})
}