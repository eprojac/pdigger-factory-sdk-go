// Command session lists what the unit's shared session can show (its recordings and its live cameras), says what
// selecting the first camera would do (a dry run: nothing changes for the other viewers), and reads the session
// clock.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./session
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx := context.Background()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	sources, err := client.Sources(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, source := range sources {
		kind := "recording"
		if source.IsLive() {
			kind = "live camera"
		}
		fmt.Printf("%-11s %s: %s\n", source.ID, kind, source.Name())
	}
	camera, err := client.Live(ctx) // the first camera
	switch {
	case pdiggerfactory.HasCode(err, pdiggerfactory.CodeLiveUnavailable):
		fmt.Println("this unit lists no camera: the shared session plays recordings")
	case err != nil:
		log.Fatal(err)
	default:
		state, err := client.Session().Watch(ctx, camera.ID, nil, pdiggerfactory.DryRun())
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("first live camera: %s (%s); watching it would start session %d (dry run: nothing changed)\n",
			camera.ID, camera.Station, state.Session)
	}
	clock, err := client.Session().Clock(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("session %d plays %s at %.1f s (playing %v)\n", clock.State.Session, clock.State.Video,
		clock.MediaTimeNow(), clock.State.Playing)
}
