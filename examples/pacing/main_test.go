package main

import (
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
)

// The program replays the fixture recording aug28-1270-1300 (30 s of media) a thousand times faster than it was
// recorded: about 30 ms, in steps of 0.2 ms, and prints the hands as the recording's worlds have them.
func TestPacing(t *testing.T) {
	extest.Args(t, "pacing", extest.Fixture("recordings", "aug28-1270-1300.ndjson.gz"), "1000")
	start := time.Now()
	got := extest.Stdout(t, main)
	if took := time.Since(start); took < 29*time.Millisecond {
		t.Fatalf("the replay was not paced: %v", took)
	}
	want := `aug28-1270-1300: 151 frames, build claude-20261122-hr4x1
21:10.0 left hand free, right hand free
21:15.4 left hand free, right hand touch
21:16.2 left hand touch, right hand touch
21:17.4 left hand touch, right hand grasp
21:17.8 left hand touch, right hand hinge
21:18.0 left hand touch, right hand carry
21:18.8 left hand carry, right hand carry
21:19.8 left hand touch, right hand carry
21:20.4 left hand carry, right hand carry
21:20.8 left hand touch, right hand carry
21:21.6 left hand free, right hand carry
21:21.8 left hand free, right hand touch
21:22.4 left hand free, right hand free
21:23.8 left hand touch, right hand free
21:25.0 left hand free, right hand free
21:29.2 left hand free, right hand touch
21:29.8 left hand free, right hand free
21:36.6 left hand touch, right hand free
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
