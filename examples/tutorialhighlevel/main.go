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
