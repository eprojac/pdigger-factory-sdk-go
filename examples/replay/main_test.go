package main

import (
	"os"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
)

// The replay quickstart prints the reference interpreter's events and summary of a fixture recording, byte for byte.
func TestReplay(t *testing.T) {
	for _, name := range []string{"aug28-1270-1300", "dry1-0296-0306-full"} {
		extest.Args(t, "replay", extest.Fixture("recordings", name+".ndjson.gz"))
		got := extest.Stdout(t, main)
		events, err1 := os.ReadFile(extest.Fixture("golden", name+".events.txt"))
		summary, err2 := os.ReadFile(extest.Fixture("golden", name+".summary.txt"))
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		if want := string(events) + string(summary); got != want {
			t.Fatalf("%s: the output differs from the golden files:\n%s", name, got)
		}
	}
}
