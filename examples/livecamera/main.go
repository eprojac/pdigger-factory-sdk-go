// Command livecamera watches a plant camera live: it selects the camera for the shared session, waits out its
// warm-up, then prints what happens.
//
// Selecting a camera changes what every viewer of the unit sees (the page follows the session). The unit connects to
// the camera, runs the interpreter on its first 3 seconds of frames to warm it up, and then shows it live. Give a
// camera's serial to watch that one; stop it with Ctrl-C (or give a number of events to stop after).
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./livecamera [serial] [events]
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	serial, stopAfter := "", -1
	for _, arg := range os.Args[1:] {
		if n, err := strconv.Atoi(arg); err == nil {
			stopAfter = n
		} else {
			serial = arg
		}
	}
	var camera *pdiggerfactory.Source
	if serial != "" {
		camera, err = client.Source(ctx, serial) // that camera
	} else {
		camera, err = client.Live(ctx) // the first camera
	}
	if err != nil {
		log.Fatal(err)
	}
	// connecting, then warming up on the camera's first 3 s
	if _, err := client.Session().Watch(ctx, camera.ID, nil); err != nil {
		log.Fatal(err)
	}
	state, err := client.Session().UntilLive(ctx, 30*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	lag := 0.0
	if state.Live != nil {
		lag = state.Live.Lag
	}
	fmt.Printf("%s (%s) is live: session %d, %.2f s behind the camera\n", camera.ID, camera.Name(), state.Session, lag)

	// the JPEG the newest frame was interpreted from
	picture, err := client.Session().Picture(ctx, pdiggerfactory.LatestFrame())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("frame g %d: a %d-byte JPEG\n", picture.G, len(picture.JPEG))

	shown := 0
	for ev, err := range client.Interpreter().Events(ctx) {
		if ctx.Err() != nil {
			return // Ctrl-C
		}
		if err != nil {
			log.Fatal(err)
		}
		at := "--:--:--"
		if ev.Wall != nil { // on a camera, the wall time is the timestamp that matters
			at = time.Unix(int64(*ev.Wall), 0).UTC().Format("15:04:05")
		}
		fmt.Printf("%s UTC  %s\n", at, ev.Describe())
		if shown++; shown == stopAfter {
			return
		}
	}
}
