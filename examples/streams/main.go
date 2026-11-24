// Command streams shows the mid level on a unit: the frame stream with its side events (it resumes by itself after
// a drop or a server restart), then the long-poll consumed as a channel, for select-based code. Both observe: they
// do not count as a viewer, so they keep nothing running on the unit.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./streams
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx := context.Background()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	frames := 0
	stream := client.Streams().Frames(ctx, pdiggerfactory.Layers(pdiggerfactory.LayerWorld), pdiggerfactory.Observe(true))
	for item, err := range stream {
		if err != nil {
			log.Print(err)
			continue
		}
		switch it := item.(type) {
		case *pdiggerfactory.FrameItem:
			frames++
			stones := 0
			if it.Frame.World != nil { // nil when the world came late (Backfill(true) waits for it)
				stones = len(it.Frame.World.Stones)
			}
			fmt.Printf("frame g %d at %s: %d stones\n", it.Frame.G, pdiggerfactory.FormatClock(it.Frame.T), stones)
		case *pdiggerfactory.ClockItem:
			fmt.Printf("clock: session %d plays %s, playing %v\n", it.State.Session, it.State.Video, it.State.Playing)
		case *pdiggerfactory.WorkerItem:
			fmt.Printf("worker: %s\n", it.Worker.State)
		case *pdiggerfactory.GapItem, *pdiggerfactory.ResetItem:
			fmt.Printf("%s: frames were skipped\n", item.Kind())
		}
		if frames == 3 {
			break // closes the connection
		}
	}

	// The same frames over the long-poll, as a channel: stop after three frames or five seconds.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	polled := pdiggerfactory.Chan(ctx, client.Streams().Poll(ctx, pdiggerfactory.Observe(true)))
	for frames = 0; frames < 3; {
		select {
		case r, ok := <-polled:
			if !ok {
				return
			}
			if fi, isFrame := r.Value.(*pdiggerfactory.FrameItem); isFrame {
				frames++
				fmt.Printf("polled frame g %d\n", fi.Frame.G)
			}
		case <-ctx.Done():
			fmt.Println("no frame within 5 s")
			return
		}
	}
}
