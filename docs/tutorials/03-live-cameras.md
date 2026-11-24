# Live cameras: one source at a time

A unit shows one source at a time, to everybody: a recording it plays, or a live camera of a plant rig. How many of
each it offers depends on the distribution of the unit (a unit can list several recordings and several cameras, or
only one of them), but there is always exactly one selected, on the one shared session that the unit's page, the
operators' screens and every SDK client follow. Selecting a camera changes what every viewer sees.

[examples/livecamera/main.go](../../examples/livecamera/main.go) watches a camera, waits out its warm-up, shows its
newest picture and follows its events:

```go
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
```

## The sources

`client.Sources(ctx)` lists what the unit can show: the recordings, which have a duration and can be sought in, and
then the cameras, by their serials (`Kind` camera, `IsLive()`), which have neither: a camera is the camera itself, at
rate 1.0. `client.Cameras(ctx)` returns only the cameras, `client.Source(ctx, id)` one by its id, and
`client.Live(ctx)` the first camera (or the error `live_unavailable`).

## Watch, warm up, go live

`Watch` starts a new session on the camera. The unit connects to it, runs the interpreter on its first seconds of frames
to warm the world up (those frames are never shown or stored), and then shows it live: the session's `Live.Phase` goes
from `connecting` to `warming` to `live`, and `UntilLive` waits for the last (the error `live_timeout` when it does not
come in time). The unit interprets only while someone watches: the watch counts as a viewer for 15 s, and the
interpreter's stream opened next keeps the camera live. To check a watch without changing anything, send it as a dry
run: `client.Session().Watch(ctx, camera.ID, nil, pdiggerfactory.DryRun())`.

## Pictures and wall times

Every frame of a camera comes with the camera's own picture, the JPEG the model saw:
`client.Session().Picture(ctx, pdiggerfactory.LatestFrame())` gives the newest one, and the unit keeps the newest 64.
The interpreter follows a camera exactly as it follows a recording; on a camera, media time only counts the seconds
since the session started, so the time that matters is each event's wall time (`Wall`, seconds since 1970).

## What differs on a camera

- A camera cannot be sought: a seek gives `not_seekable`, and the SDK refuses one locally when the shared state shows
  a camera. `Pause` freezes the shared view, and `Play` goes on at the live edge, after a new warm-up.
- A camera's frames come at the camera's own rate as far as the worker keeps up (where a recording gives 5 a second):
  on the 20.20 unit 1.6 to 3.9 a second on build `claude-20261120-looks1`, and about 3.6 a second on build
  `claude-20261122-hr4x1`, as fast as its worker goes on a camera frame. `G` counts them from the start of the session.
- For your tests, the fake unit offers a camera, `cam-live`, made from its recording, with its phases and pictures:
  `factorytest.WithLiveCamera()` in process, or `make fake-unit ARGS=-live` on 127.0.0.1 ([testing.md](../testing.md)).

The details are in [concepts.md](../concepts.md), and [BEHAVIOUR-20.20.md](../BEHAVIOUR-20.20.md) has the 20.20
unit's cameras, measured.
