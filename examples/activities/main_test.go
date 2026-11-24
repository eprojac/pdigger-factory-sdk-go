package main

import (
	"os"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// The program follows a fake unit playing the fixture recording dry1-0000-0130 whose first stream drops after 100
// events: the activities come through the reconnect as the reference interpreter has them.
func TestActivities(t *testing.T) {
	fake := extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0000-0130.ndjson.gz")))
	fake.DropAfter("streamFrames", 100)
	golden, err := os.ReadFile(extest.Fixture("golden", "dry1-0000-0130.activities.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(string(golden), "\n")[:12]
	got := extest.Lines(t, main, len(want), extest.Interrupt)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	streams := 0
	for _, op := range fake.Ops() {
		if op == "streamFrames" {
			streams++
		}
	}
	if streams < 2 {
		t.Fatalf("the stream did not resume: %v", fake.Ops())
	}
}
