package liftcount

import (
	"context"
	"net/http"
	"testing"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestLifts(t *testing.T) {
	// A fake unit whose session plays the recording embedded in factorytest (aug28 from 21:10.0, one lift); the
	// client talks to it in process.
	fake, err := factorytest.New(factorytest.WithSampleRecording())
	if err != nil {
		t.Fatal(err)
	}
	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
	if err != nil {
		t.Fatal(err)
	}
	// Faults: the first stream drops after 20 events, the next connection attempt fails with a 503.
	fake.DropAfter("streamFrames", 20)
	fake.FailNext("streamFrames", 1, factorytest.ErrorReply(503, "busy", "try again"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lifts, err := Lifts(ctx, client, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := "21:17.8 Right hand lifts stone 2660"; len(lifts) != 1 || lifts[0] != want {
		t.Fatalf("got %q, want %q", lifts, want)
	}
	if ops := fake.Ops(); len(ops) < 3 {
		t.Fatalf("the stream did not resume: %v", ops)
	}
}

// TestFrameReadsThatHang: the unit hangs on every read of a frame. The SDK gives up on each read after its timeout on
// the client's clock, which the fake's delay hook moves on, so the interpreter goes on and the test waits for nothing.
func TestFrameReadsThatHang(t *testing.T) {
	// dry2 from 9:42.0: 13 of its frames came without their world, and the interpreter tries to read each one back
	fake, err := factorytest.New(factorytest.WithRecording(extest.Fixture("recordings", "dry2-0582-0600.ndjson.gz")))
	if err != nil {
		t.Fatal(err)
	}
	clock := factorytest.NewClock(time.Time{})
	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	fake.SetDelay(func(op string, r *http.Request) {
		if op == "getFrame" {
			clock.Advance(time.Second) // the backfill read's timeout: the SDK gives up on the read
			<-r.Context().Done()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// an event after the frames without a world: they were released as they came, in order
	late := func(e pdiggerfactory.Event) bool { return e.T > 598 }
	event, err := client.Interpreter().WaitFor(ctx, late, 0)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, op := range fake.Ops() {
		if op == "getFrame" {
			reads++
		}
	}
	if event.T <= 598 || reads < 3 {
		t.Fatalf("%s after %d reads", event.Describe(), reads)
	}
}
