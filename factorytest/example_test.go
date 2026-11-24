package factorytest_test

import (
	"context"
	"fmt"
	"log"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// A client of a fake unit that plays the sample recording embedded in this package, in process and on a manual
// clock, with a fault: the first frame stream drops after 10 events. The interpreter resumes the stream by itself.
func ExampleNew() {
	fake, err := factorytest.New(factorytest.WithSampleRecording())
	if err != nil {
		log.Fatal(err)
	}
	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
	if err != nil {
		log.Fatal(err)
	}
	fake.DropAfter("streamFrames", 10)
	lift, err := client.Interpreter().WaitFor(context.Background(), pdiggerfactory.OfType(pdiggerfactory.EventLift),
		time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(lift.Describe())
	streams := 0
	for _, op := range fake.Ops() {
		if op == "streamFrames" {
			streams++
		}
	}
	fmt.Println("frame streams opened:", streams)
	// Output:
	// 21:17.8 Right hand lifts stone 2660
	// frame streams opened: 2
}

// Faults: the next request of an operation gets an error reply; the SDK retries a 503 by itself.
func ExampleFake_FailNext() {
	fake, err := factorytest.New(factorytest.WithSampleRecording())
	if err != nil {
		log.Fatal(err)
	}
	clock := factorytest.NewClock(time.Time{})
	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(clock), pdiggerfactory.WithRetries(1))
	if err != nil {
		log.Fatal(err)
	}
	fake.FailNext("getSession", 1, factorytest.ErrorReply(503, "busy", "try again"))
	state, err := client.Session().State(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(state.Video, "after", len(clock.Sleeps()), "retry")
	fake.FailNext("getSession", 2, factorytest.ErrorReply(503, "busy", "try again"))
	_, err = client.Session().State(context.Background())
	fmt.Println(pdiggerfactory.AsError(err).Code)
	// Output:
	// aug28 after 1 retry
	// busy
}
