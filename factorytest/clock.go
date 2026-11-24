package factorytest

import (
	"context"
	"sync"
	"time"
)

// Clock is a manual clock for the SDK's WithClock and ReplayOptions.Clock: Sleep advances it at once instead of
// waiting (unless the context is done), so backoff, backfill and pacing run instantly and deterministically. Its
// AfterFunc timers (the streams' ConnectionMaxSeconds) fire when Sleep or Advance moves the clock past them.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
	timers []*timer
}

// timer is a pending AfterFunc.
type timer struct {
	at time.Time
	f  func()
}

// NewClock makes a Clock that starts at start (the zero time means 2026-11-18 12:00 UTC).
func NewClock(start time.Time) *Clock {
	if start.IsZero() {
		start = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	}
	return &Clock{now: start}
}

// Now is the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Sleep records d and advances the clock by it; it returns the context's error if the context is done.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	if d > 0 {
		c.now = c.now.Add(d)
		c.fire()
	}
	return nil
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.fire()
}

// AfterFunc calls f in its own goroutine once the clock has been moved d past now (at once when d <= 0); stop cancels
// the call and reports whether it did so before f was called.
func (c *Clock) AfterFunc(d time.Duration, f func()) (stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &timer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	c.fire()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		for i, p := range c.timers {
			if p == t {
				c.timers = append(c.timers[:i], c.timers[i+1:]...)
				return true
			}
		}
		return false
	}
}

// Timers is the number of AfterFunc calls still waiting.
func (c *Clock) Timers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// fire starts the timers that are due. The caller holds c.mu.
func (c *Clock) fire() {
	waiting := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			waiting = append(waiting, t)
			continue
		}
		go t.f()
	}
	c.timers = waiting
}

// Sleeps are the durations Sleep was asked for, in order.
func (c *Clock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}
