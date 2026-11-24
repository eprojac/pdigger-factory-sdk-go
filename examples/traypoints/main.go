// Command traypoints lists the trays of a recording: when each had arrived and settled, and when it was about to move
// out, with what was removed from it. Then it shows one tray on the shared session: a seek to its arrival and one to
// its leaving point.
//
// The tray stones carry the SDK's own ids, one per stone and never reused, so "stone X was removed" holds over time;
// the world's ids are the engine tracker's, which renames stones and reuses ids. A seek changes what every viewer
// sees, so this example sends dry runs only.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./traypoints        # dry1 (published), tray 2
//	PDIGGER_FACTORY_URL=… go run ./traypoints ../fixtures/recordings/dry1-0018-0153.ndjson.gz 1
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
	source, show := "https://factory.pdigger.com/developers/data/dry1.world.ndjson.gz", int64(2)
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	if len(os.Args) > 2 {
		n, err := strconv.ParseInt(os.Args[2], 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		show = n
	}
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, source, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	trays, err := replay.Trays(ctx)
	if err != nil {
		log.Fatal(err)
	}
	var shown *pdiggerfactory.Tray
	for i := range trays {
		tray := &trays[i]
		arrived, leaving, ok := tray.Window()
		if !ok {
			continue // a stop that never settled
		}
		fmt.Printf("tray %d: arrived %s, leaving %s\n", tray.N, pdiggerfactory.FormatClock(arrived),
			pdiggerfactory.FormatClock(leaving))
		for _, stone := range tray.Stones {
			if stone.Status == pdiggerfactory.TrayStoneRemoved && *stone.Certain {
				fmt.Println("  removed:", stone.ID)
			}
		}
		if tray.N == show && shown == nil {
			shown = tray
		}
	}
	if shown == nil {
		log.Fatalf("no tray %d that arrived and left", show)
	}
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	for _, point := range []*pdiggerfactory.TrayPoint{shown.Arrived, shown.Leaving} {
		would, err := client.Session().Seek(ctx, point.T, pdiggerfactory.InVideo(shown.Video), pdiggerfactory.DryRun())
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("seek to %s would show %s from %s\n", pdiggerfactory.FormatClock(point.T), would.Video,
			pdiggerfactory.FormatClock(would.AnchorMedia))
	}
}
