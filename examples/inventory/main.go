// Command inventory feeds frames to the interpreter's pure state machine, InterpreterCore, itself: here the frames
// of a recording, in a program of yours frames from anywhere (ones you stored, or read with GetFrame). It prints the
// stone inventory of the last frame: every stone with its shape and its position on the board.
//
//	go run ./inventory ../fixtures/recordings/dry2-0582-0600.ndjson.gz
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: inventory <recording>")
	}
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, os.Args[1], nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	core := pdiggerfactory.NewInterpreterCore()
	events := 0
	for frame, err := range replay.Frames(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		update := core.Feed(frame) // synchronous: no I/O, no goroutines
		events += len(update.Events)
	}
	state := core.State()
	fmt.Printf("%s at %s, after %d events: %d stones\n", state.Video, pdiggerfactory.FormatClock(state.T), events,
		len(state.Stones))
	for _, id := range state.StoneIDs() {
		stone := state.Stone(id)
		if stone.Solid == nil || len(stone.Solid.Pos) < 2 {
			fmt.Printf("stone %d: %s, %s, no 3-D pose yet\n", id, stone.Family, stone.Shape)
			continue
		}
		x, y := stone.Solid.Pos[0], stone.Solid.Pos[1] // scene x and y, in units of the template gauge
		fmt.Printf("stone %d: %s, %s, at (%s, %s)\n", id, stone.Family, stone.Shape,
			pdiggerfactory.FormatFixed(x, 2), pdiggerfactory.FormatFixed(y, 2))
	}
}
