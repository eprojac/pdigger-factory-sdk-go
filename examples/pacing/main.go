// Command pacing replays a recording at the pace it was recorded, as if a unit were playing it now: each update
// comes at its frame's media time (divided by the speed). It prints both hands whenever one of them changes state.
//
//	go run ./pacing ../fixtures/recordings/aug28-1270-1300.ndjson.gz      # in real time
//	go run ./pacing ../fixtures/recordings/aug28-1270-1300.ndjson.gz 10   # ten times faster
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
	if len(os.Args) < 2 {
		log.Fatal("usage: pacing <recording> [speed]")
	}
	speed := 1.0
	if len(os.Args) > 2 {
		var err error
		if speed, err = strconv.ParseFloat(os.Args[2], 64); err != nil {
			log.Fatal(err)
		}
	}
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, os.Args[1], &pdiggerfactory.ReplayOptions{
		Pace: pdiggerfactory.PaceRealtime, Speed: speed})
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	if h := replay.Header; h != nil {
		fmt.Printf("%s: %d frames, build %s\n", h.Name, h.Frames, h.Build)
	}
	var last pdiggerfactory.Hands
	for update, err := range replay.Updates(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		hands := update.State.Hands
		if !update.Final && (hands.Left.State != last.Left.State || hands.Right.State != last.Right.State) {
			fmt.Printf("%s left hand %s, right hand %s\n", pdiggerfactory.FormatClock(update.State.T),
				hands.Left.State, hands.Right.State)
			last = hands
		}
	}
}
