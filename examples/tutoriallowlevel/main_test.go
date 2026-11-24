package main

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// A fake unit playing dry1-0296-0306-full (all five layers), with the replies recorded on 20.20 for the operations
// the fake does not compute (status, model, renders): at frame 1510 the program prints what the other languages' tutorial
// programs print there.
func TestTutorialLowLevel(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0296-0306-full.ndjson.gz")),
		factorytest.WithReplies(extest.Fixture("api")))
	extest.Args(t, "tutoriallowlevel", "1510")
	want := `ready true, worker idle: 3.8 frames a second, 241.6 ms each
checkpoint 8d09531c915f on trt: 6 engines
70 joints, 62 bones, 11 colours
stone 627: 51 points, g 1480 to 1530
focus on stone 627: dry run true
frame g 1510 at 5:01.2: 42 stones, rect
maps of g 1510: 40 x 64 cells, 920 of the tray, 1169 of stones
overlay of g 3079: 44 polygons, 7 segments, 8 circles at 1920 x 1080 px
scene of g 3079: 42 stones, 7 bones
g 1510: 42 outlines, 168 points
g 1510: 50 of 62 bones
left hand: Hand on a stone, wrist at 1528, 595 px
right hand: Holding a stone, wrist at 1270, 415 px
regions of g 1510: a 1106-byte PNG
picture: /api/v2/videos/dry1/media at 5:01.2, 960 x 540
`
	if got := extest.Stdout(t, main); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// On a live camera the last step fetches the picture the frame was read from: the fake unit's camera, watched and
// warmed up first, and the newest frame.
func TestTutorialLowLevelOnACamera(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0296-0306-full.ndjson.gz")),
		factorytest.WithReplies(extest.Fixture("api")), factorytest.WithLiveCamera(),
		factorytest.WithClock(pdiggerfactory.SystemClock{}))
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.Session().Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Session().UntilLive(ctx, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	extest.Args(t, "tutoriallowlevel")
	out := strings.Split(strings.TrimSuffix(extest.Stdout(t, main), "\n"), "\n")
	if len(out) != 15 || !regexp.MustCompile(`^picture: a [0-9]+-byte JPEG of g [0-9]+$`).MatchString(out[14]) {
		t.Fatal(strings.Join(out, "\n"))
	}
}
