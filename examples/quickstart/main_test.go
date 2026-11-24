package main

import (
	"os"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// The quickstart follows a fake unit whose session plays the fixture recording aug28-1270-1300, and prints the same
// sentences as the reference interpreter until it is interrupted.
func TestQuickstart(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "aug28-1270-1300.ndjson.gz")))
	golden, err := os.ReadFile(extest.Fixture("golden", "aug28-1270-1300.events.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(string(golden), "\n")[:12]
	got := extest.Lines(t, main, len(want), extest.Interrupt)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
