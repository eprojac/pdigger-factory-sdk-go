package pdiggerfactory

import (
	"context"
	"time"
)

// Clock is the time source of a Client and a Replay: backoff waits, late-layer backfill and the timeout of its
// reads, the timeout of the streams' clock read on connect (ClockOnConnect), the streams' connection timer
// (ConnectionMaxSeconds), realtime pacing and the session clock use it. Tests inject a fake one (factorytest.Clock);
// the default is the system clock. (The request timeout of WithTimeout is real time.)
type Clock interface {
	// Now is the current local time.
	Now() time.Time
	// Sleep waits for d or until ctx is done, whichever comes first, and returns ctx.Err() in the second case.
	Sleep(ctx context.Context, d time.Duration) error
}

// TimerClock is a Clock that can also call a function once a duration has passed on it, as time.AfterFunc does.
// SystemClock and factorytest.Clock are TimerClocks. The streams time ConnectionMaxSeconds and the timeouts of the
// backfill and clock reads with AfterFunc when the client's clock has it, and with a Sleep in a goroutine when it does
// not (so with a clock whose Sleep returns at once, they end at once).
type TimerClock interface {
	Clock
	// AfterFunc calls f in its own goroutine once d has passed; stop cancels the call and reports whether it did so
	// before f was called.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep waits for d or until ctx is done.
func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// AfterFunc is time.AfterFunc.
func (SystemClock) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

// afterFunc calls f once d has passed on clk; the func it returns cancels the call.
func afterFunc(clk Clock, d time.Duration, f func()) func() {
	if tc, ok := clk.(TimerClock); ok {
		stop := tc.AfterFunc(d, f)
		return func() { stop() }
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if clk.Sleep(ctx, d) == nil {
			f()
		}
	}()
	return cancel
}

// withTimeout is context.WithTimeout on clk: the context ends once d has passed on clk (with a manual clock, such as
// factorytest.Clock, when the clock is moved past it), or when ctx ends.
func withTimeout(ctx context.Context, clk Clock, d time.Duration) (context.Context, context.CancelFunc) {
	tctx, cancel := context.WithCancel(ctx)
	stop := afterFunc(clk, d, cancel)
	return tctx, func() {
		stop()
		cancel()
	}
}

// epoch converts a time to epoch seconds as a float, the unit of the API's wall times.
func epoch(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}
