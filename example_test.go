package pdiggerfactory_test

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// The examples run against the SDK's fake unit (factorytest) and the fixture recordings (of the 20.20 unit, and of a
// replay of its model and build on a Mac), so their output is real; with a unit, give pdiggerfactory.New its URL
// instead (http://factory-orin.local:8120).

// sampleFake is one fake unit for the examples (none of them moves its session), made on first use.
var sampleFake = sync.OnceValue(func() *factorytest.Fake {
	fake, err := factorytest.New(factorytest.WithSampleRecording())
	if err != nil {
		log.Fatal(err)
	}
	return fake
})

// sampleClient is a client of a fake unit whose shared session plays the sample recording (aug28 from 21:10.0).
func sampleClient() *pdiggerfactory.Client {
	fake := sampleFake()
	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
	if err != nil {
		log.Fatal(err)
	}
	return client
}

// Follow the shared session and print each event as a sentence.
func ExampleInterpreter_Events() {
	client := sampleClient()
	ctx := context.Background()
	n := 0
	for event, err := range client.Interpreter().Events(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(event.Describe())
		if n++; n == 3 {
			break // closes the stream
		}
	}
	// Output:
	// 21:10.0 stone 2629 gets its final shape
	// 21:10.2 stone 2620 gets its final shape
	// 21:10.8 stone 2611 gets its final shape
}

// Wait for the next lift, then follow the activities until the lifted stone's move ends.
func ExampleInterpreter_WaitFor() {
	client := sampleClient()
	ctx := context.Background()
	interpreter := client.Interpreter()
	lift, err := interpreter.WaitFor(ctx, pdiggerfactory.OfType(pdiggerfactory.EventLift), time.Minute)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(lift.Describe())
	for activity, err := range interpreter.Activities(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		if activity.Kind == pdiggerfactory.KindMove && *activity.StoneID == *lift.StoneID {
			fmt.Println(activity.Describe(), "->", activity.Outcome)
			break
		}
	}
	// Output:
	// 21:17.8 Right hand lifts stone 2660
	// 21:17.8 Right hand moves stone 2660 0.84 units in 4.0 s -> placed
}

// Replay a recording offline: its activities and summary.
func ExampleOpenReplay() {
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, "fixtures/recordings/aug28-1270-1300.ndjson.gz", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	result, err := replay.Run(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, activity := range result.Activities {
		fmt.Println(activity.Describe())
	}
	fmt.Println(result.Summary.Frames, "frames,", result.Summary.Moves.Total, "move")
	// Output:
	// 21:16.2 Right hand pushes stones 2640, 2641, 2643, 2649 and 2653 (10 pushes in 1.4 s)
	// 21:17.6 Left hand pushes stones 2640 and 2664 (2 pushes in 0.4 s)
	// 21:18.2 Both hands push stones 2638 and 2640 (2 pushes in 0.8 s)
	// 21:17.8 Right hand moves stone 2660 0.84 units in 4.0 s
	// 21:24.8 Left hand pushes stone 2708 (1 push)
	// 21:37.0 Left hand pushes stone 2709 (3 pushes in 0.8 s)
	// 21:39.0 Left hand pushes stones 2708 and 2709 (4 pushes in 0.8 s)
	// 151 frames, 1 move
}

// One report per tray, simple mode: which stones were removed, added and moved, when and where, with the probability
// that each change happened, and whether that is the tray's whole account (here 18 changes are unclear and 1 stone is
// not sure: the tray's facts say why).
func ExampleReplay_TrayReports() {
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, "fixtures/recordings/dry1-0018-0153.ndjson.gz", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	reports, err := replay.TrayReports(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, report := range reports {
		fmt.Println(report.Describe())
	}
	// Output:
	// tray 1 (2026-11-22T02:38:47.0Z), 0:22.5 to 2:24.0: 11 removed, 13 added, 8 moved, not complete: 18 unclear, 1 not sure
	//   0:45.5 removed 20261122T023847.0Z-1-117 from (0.68, -1.06) (0.98)
	//   0:45.5 removed 20261122T023847.0Z-1-127 from (1.14, -1.24) (0.98)
	//   0:59.4 removed 20261122T023847.0Z-1-126 from (0.35, -1.29) (0.98)
	//   1:05.4 removed 20261122T023847.0Z-1-105 from (0.49, -0.82) (0.98)
	//   1:23.6 removed 20261122T023847.0Z-1-112 from (-0.11, -0.84) (0.98)
	//   1:23.6 removed 20261122T023847.0Z-1-125 from (-0.09, -1.26) (0.98)
	//   2:24.0 removed 20261122T023847.0Z-1-87 from (0.02, -0.17) (unclear, 0.71)
	//   2:24.0 removed 20261122T023847.0Z-1-121 from (0.33, -1.27) (unclear, 0.71)
	//   2:24.0 removed 20261122T023847.0Z-1-128 from (0.56, -1.29) (unclear, 0.71)
	//   2:24.0 removed 20261122T023847.0Z-1-129 from (0.74, -1.29) (unclear, 0.71)
	//   2:24.0 removed 20261122T023847.0Z-1-130 from (0.87, -1.27) (unclear, 0.71)
	//   2:24.0 added 20261122T023943.4Z-1-151 at (0.16, -1.26) (unclear, 0.52)
	//   2:24.0 added 20261122T024104.0Z-1-177 at (0.02, -0.82) (unclear, 0.96)
	//   2:24.0 added 20261122T024018.8Z-1-193 at (0.36, -0.81) (unclear, 0.96)
	//   2:24.0 added 20261122T024029.7Z-1-201 at (0.92, -0.14) (unclear, 0.96)
	//   2:24.0 added 20261122T024031.0Z-1-203 at (1.08, -0.15) (unclear, 0.96)
	//   2:24.0 added 20261122T024051.0Z-1-246 at (-0.20, -1.28) (unclear, 0.52)
	//   2:24.0 added 20261122T024055.2Z-1-249 at (-0.01, -1.24) (unclear, 0.52)
	//   2:24.0 added 20261122T024108.5Z-1-278 at (0.34, -1.23) (unclear, 0.52)
	//   2:24.0 added 20261122T024118.1Z-1-279 at (0.50, -1.23) (unclear, 0.96)
	//   2:24.0 added 20261122T024121.3Z-1-280 at (0.76, -1.21) (unclear, 0.96)
	//   2:24.0 added 20261122T024121.3Z-1-281 at (0.63, -1.22) (unclear, 0.96)
	//   2:24.0 added 20261122T024124.2Z-1-282 at (0.99, -1.01) (unclear, 0.96)
	//   2:24.0 added 20261122T024126.9Z-1-284 at (0.99, -1.22) (unclear, 0.96)
	//   moved 20261122T023847.0Z-1-85 0.33 to (-0.12, -0.18) (0.87)
	//   moved 20261122T023847.0Z-1-86 0.40 to (0.72, -0.14) (0.87)
	//   moved 20261122T023847.0Z-1-88 0.35 to (0.11, -0.17) (0.87)
	//   moved 20261122T023847.0Z-1-89 0.36 to (0.34, -0.16) (0.87)
	//   moved 20261122T023847.0Z-1-90 0.38 to (0.51, -0.15) (0.87)
	//   moved 20261122T023847.0Z-1-104 0.38 to (-0.25, -0.84) (0.87)
	//   moved 20261122T023847.0Z-1-118 0.40 to (0.50, -1.02) (0.87)
	//   moved 20261122T023847.0Z-1-120 0.43 to (0.75, -1.00) (0.87)
}

// Feed the pure state machine by hand: here from a recording's frames.
func ExampleInterpreterCore() {
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, "fixtures/recordings/dry2-0582-0600.ndjson.gz", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	core := pdiggerfactory.NewInterpreterCore()
	for frame, err := range replay.Frames(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		core.Feed(frame)
	}
	state := core.State()
	fmt.Printf("g %d at %s: %d stones, left hand %s\n", state.G, pdiggerfactory.FormatClock(state.T),
		len(state.Stones), state.Hands.Left.State)
	// Output:
	// g 3009 at 10:00.1: 42 stones, left hand free
}

// Frames with their side events, as the unit releases them.
func ExampleStreams_Frames() {
	client := sampleClient()
	ctx := context.Background()
	frames := 0
	for item, err := range client.Streams().Frames(ctx, pdiggerfactory.Layers(pdiggerfactory.LayerWorld)) {
		if err != nil {
			log.Fatal(err)
		}
		switch it := item.(type) {
		case *pdiggerfactory.ClockItem:
			fmt.Printf("clock: %s, playing %v at rate %v\n", it.State.Video, it.State.Playing, it.State.Rate)
		case *pdiggerfactory.FrameItem:
			fmt.Printf("frame g %d: %d stones\n", it.Frame.G, len(it.Frame.World.Stones))
			frames++
		}
		if frames == 2 {
			break
		}
	}
	// Output:
	// clock: aug28, playing true at rate 0.8
	// frame g 6350: 33 stones
	// frame g 6351: 33 stones
}

// A dry run checks a control without moving the shared session.
func ExampleSession_Watch() {
	client := sampleClient()
	ctx := context.Background()
	state, err := client.Session().Watch(ctx, "aug28", pdiggerfactory.Float64(1275), pdiggerfactory.DryRun())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("would play", state.Video, "from", pdiggerfactory.FormatClock(state.AnchorMedia))
	_, err = client.Session().Watch(ctx, "aug28", pdiggerfactory.Float64(-5), pdiggerfactory.DryRun())
	fmt.Println("refused:", pdiggerfactory.AsError(err).Code)
	// Output:
	// would play aug28 from 21:15.0
	// refused: time_out_of_range
}

// Error handling: branch on the stable code.
func ExampleError() {
	client := sampleClient()
	_, err := client.Source(context.Background(), "jul31")
	if e := pdiggerfactory.AsError(err); e != nil {
		fmt.Println(e.Status, e.Code, e.Field, pdiggerfactory.IsRetryable(err))
	}
	// Output:
	// 404 unknown_video id false
}

func ExampleFormatClock() {
	fmt.Println(pdiggerfactory.FormatClock(5.2), pdiggerfactory.FormatClock(1277.8), pdiggerfactory.FormatClock(3723.4))
	// Output:
	// 0:05.2 21:17.8 1:02:03.4
}

func ExampleFormatFixed() {
	fmt.Println(pdiggerfactory.FormatFixed(0.25, 1), pdiggerfactory.FormatFixed(0.7155, 2), pdiggerfactory.FormatFixed(-1.5, 0))
	// Output:
	// 0.3 0.72 -2
}

func ExampleEvent_Describe() {
	event := pdiggerfactory.Event{Type: pdiggerfactory.EventLift, T: 1277.8, Hand: pdiggerfactory.HandRight,
		StoneID: pdiggerfactory.Int64(2660)}
	both := pdiggerfactory.Event{Type: pdiggerfactory.EventPush, T: 20.5, Hand: pdiggerfactory.HandBoth,
		StoneID: pdiggerfactory.Int64(94)}
	fmt.Println(event.Describe())
	fmt.Println(both.Describe())
	// Output:
	// 21:17.8 Right hand lifts stone 2660
	// 0:20.5 Both hands push stone 94
}
