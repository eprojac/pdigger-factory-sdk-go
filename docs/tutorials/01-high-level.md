# High level: what the hands did

The interpreter turns the unit's frames into what a person would say happened at the work area: events
(`1:19.2 Left hand lifts stone 155`), activities (a hand moving one stone, or a burst of pushes), the state of both
hands, the stones in view, and the trays that pass through. It is the same interpreter for a recording replayed
offline and for a unit followed live, and all four languages give the same sentences for the same frames.

This tutorial replays a recording first, so it needs no unit, and then follows one. Its program is
[examples/tutorialhighlevel/main.go](../../examples/tutorialhighlevel/main.go):

```go
// Command tutorialhighlevel is the high-level tutorial (docs/tutorials/01-high-level.md) in one program, offline: a
// recording's activities, as sentences and as data; its tray facts, and its trays with the stones removed from them;
// and one pass with a core of your own, for the summary and a stone's path. Every iteration of a Replay reads the
// recording again from the start, streamed: only the interpreter's state is in memory.
//
//	go run ./tutorialhighlevel ../fixtures/recordings/dry1-0018-0153.ndjson.gz 155
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx := context.Background()
	source, stone := "../fixtures/recordings/dry1-0018-0153.ndjson.gz", int64(155)
	if len(os.Args) > 1 {
		source = os.Args[1] // a path, or an http(s) URL such as a published recording
	}
	if len(os.Args) > 2 {
		if id, err := strconv.ParseInt(os.Args[2], 10, 64); err == nil {
			stone = id
		}
	}
	replay, err := pdiggerfactory.OpenReplay(ctx, source, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()

	// 1. The activities, as sentences
	for activity, err := range replay.Activities(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(activity.Describe()) // 1:19.2 Left hand takes stone 155 away (1.0 s)
	}

	// 2. The same activities as data: a move is one hand moving one stone; a push is a burst of pushes
	for activity, err := range replay.Activities(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		if activity.Kind != pdiggerfactory.KindMove {
			continue
		}
		id, hand := "?", activity.Hand
		if activity.StoneID != nil {
			id = strconv.FormatInt(*activity.StoneID, 10)
		}
		if hand == "" {
			hand = "no" // no event named a side
		}
		at := pdiggerfactory.FormatClock(activity.Start.T)
		fmt.Printf("%s stone %s: %s hand, %s, %.1f s\n", at, id, hand, activity.Outcome, activity.Duration)
	}

	// 3. Tray facts: what happened to each tray and its stones, under stone ids of the SDK's own
	for fact, err := range replay.TrayFacts(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(fact.Describe()) // 0:22.5 tray 1 arrived: 42 stones
	}

	// 4. The trays, with their two points: arrived and settled, and the last calm frame before leaving
	trays, err := replay.Trays(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, tray := range trays {
		arrived, leaving, ok := tray.Window()
		if !ok {
			continue // a stop that never settled
		}
		gone := 0
		for _, s := range tray.Stones {
			if s.Status == pdiggerfactory.TrayStoneRemoved && s.Certain != nil && *s.Certain {
				gone++
			}
		}
		from, to := pdiggerfactory.FormatClock(arrived), pdiggerfactory.FormatClock(leaving)
		fmt.Printf("tray %d: %s to %s, %d stones removed for certain\n", tray.N, from, to, gone)
	}

	// 5. One pass with a core of your own: its summary, and where a stone of the world was, frame by frame
	core := pdiggerfactory.NewInterpreterCore()
	for item, err := range replay.Items(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		switch it := item.(type) {
		case *pdiggerfactory.ClockItem: // the session's clock: the events' wall times
			core.SetClock(&pdiggerfactory.ClockEvent{State: it.State, ServerNow: it.ServerNow})
		case *pdiggerfactory.GapItem, *pdiggerfactory.ResetItem: // frames the stream dropped: the tray rules could not read them
			core.Gap()
		case *pdiggerfactory.FrameItem:
			core.Feed(it.Frame) // an Update: the frame's events, activities and tray facts
		}
	}
	core.End() // closes the activities and the tray still open at the end
	fmt.Print(core.Summary().Describe())
	if track := core.Track(stone); len(track) > 0 { // scene units
		first, last := pdiggerfactory.FormatClock(track[0].T), pdiggerfactory.FormatClock(track[len(track)-1].T)
		fmt.Printf("stone %d: %d points from %s to %s\n", stone, len(track), first, last)
	}
}
```

`OpenReplay` reads the recording's header. Each of its iterators is then one pass over the file, streamed, with an
interpreter of its own: only the interpreter's state is in memory, so a recording of hours works the same, and
cancelling the context ends a pass at once. Run the program from `examples/` on the fixture
`../fixtures/recordings/dry1-0018-0153.ndjson.gz`, dry1 from 0:18 to 2:33, where a tray arrives with 42 stones, the
hands take some off and push the others into place; any published recording works the same, its URL as the argument.

## 1. The activities

The first activities of dry1-0018-0153:

```text
0:20.5 Both hands push stones 86, 90 and 94 (6 pushes in 0.6 s)
0:20.3 Right hand pushes stones 89, 90, 94, 95 and 109 (13 pushes in 1.6 s)
0:21.9 Left hand pushes stone 86 (1 push)
```

## 2. The same activities as data

A sentence is for people; your program reads the fields. An `Activity` of kind `KindMove` is one stone moved by one
hand, with where it went (`Outcome`: `placed`, `dropped`, `carried_away`, `removed`, or `interrupted` when the
recording or the session ended first); one of kind `KindPush` is a burst of pushes, with the pushed `Stones` and the
number of `Pushes`. In dry1-0018-0153 that is nine moves, among them
`1:19.2 stone 155: left hand, carried_away, 1.0 s`: five stones placed, two dropped and two carried away.

## 3. Tray facts

The world's stone ids belong to the engine's tracker, which renames stones and reuses ids. For trays the SDK keeps
ids of its own, one per stone and never reused, so that "this stone was removed" stays true over time. Its tray facts
say what happened to each tray and its stones; the first removals from dry1-0018-0153's tray:

```text
0:45.5 tray 1: stone 20261122T023847.0Z-1-117 was removed
0:45.5 tray 1: stone 20261122T023847.0Z-1-127 was removed
```

and its last fact, as it moves out: `2:24.0 tray 1 leaving: 44 stones (30 sure), 0 added, 6 removed, 18 unclear`.

## 4. The trays

A tray has two points to jump to: when it had arrived and settled, and its last calm frame before it moved out.
`Window()` gives both, and each tray stone says whether it was removed (`Status`), and whether that is certain
(`Certain`). For dry1-0018-0153: `tray 1: 0:22.5 to 2:24.0, 6 stones removed for certain`.

## 5. One pass with a core of your own

The interpreter's rules live in `InterpreterCore`, which does no I/O: feed it frames from anywhere, here the
recording's items (its clock lines give the events their wall times, and its `gap` and `reset` lines, which `Gap()`
passes on, tell the tray rules where frames were dropped), and each `Feed` hands back an `Update` with the frame's
events, activities and tray facts. After `End()`, ask the core for its summary, its `State()`, or the path of
any stone of the world (`Track`). The summary of dry1-0018-0153 begins:

```text
frames: 676 (with world 676, without 0, revisions 0)
media: 2:14.6, sessions 1, gaps 0
stones: last 42, max 49, confirmed 42
moves: 9 (placed 5, dropped 2, carried away 2, removed 0, interrupted 0, partial 0, carried 6), mean 1.4 s, 0.18 units
```

and stone 155's path has 5 points, from 1:15.6 to 1:19.8. For the summary alone, `replay.Summary(ctx)` is one call;
`replay.Run(ctx)` gives a pass's events, activities, tray facts, trays and summary at once.

## 6. Follow a unit, live

The same interpreter follows the unit's shared session, whatever it shows: a recording it plays, or a live camera.
[examples/quickstart/main.go](../../examples/quickstart/main.go) prints its events as they happen:

```go
// Command quickstart follows the unit's shared session and prints each event as a sentence, until Ctrl-C, whatever the
// session shows: a recording, or one of the plant cameras live (./livecamera selects one). The program is the same.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstart
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt) // Ctrl-C ends the loop
	defer stop()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL")) // e.g. http://factory-orin.local:8120
	if err != nil {
		log.Fatal(err)
	}
	for event, err := range client.Interpreter().Events(ctx) {
		if err != nil {
			log.Print(err) // a frame that did not decode (the stream goes on) or the error that ended it
			continue
		}
		fmt.Println(event.Describe()) // 21:17.8 Right hand lifts stone 2660
	}
}
```

The stream resumes by itself when the connection drops, holds a frame whose world came late until it is read back,
and gives every event its wall time from the session's clock. To follow the activities as they complete, read the
hands and the summary, and wait for one event with a time limit, see
[examples/activities](../../examples/activities/main.go),
[examples/interpreter](../../examples/interpreter/main.go) and [interpreter.md](../interpreter.md).

## 7. Show a tray on the shared screen

Every viewer of the unit sees the same session. To show a tray there, seek to one of its points:
[examples/traypoints](../../examples/traypoints/main.go) lists a recording's trays and asks the unit with dry runs
(`pdiggerfactory.DryRun()`), which it checks and does not apply; leave the option out to really move the session for
everybody.

## 8. Test your program without a unit

The package `factorytest` is a fake unit that answers the whole API from a recording, in process: see
[the Go tutorial](04-go.md#the-fake-unit-in-your-tests).

## Next

- [Low level](02-low-level.md): the operations under the interpreter, one call each.
- [Live cameras](03-live-cameras.md): select a plant camera and follow it.
- The reference: [interpreter.md](../interpreter.md) (every event, activity, tray fact and the summary) and
  [replay.md](../replay.md) (replays, pacing, the recording format).
