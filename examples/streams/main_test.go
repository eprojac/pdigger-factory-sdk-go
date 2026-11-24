package main

import (
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestStreams(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "joined-mid-move.ndjson.gz")))
	want := `clock: session 1795314878 plays dry1, playing true
frame g 399 at 1:19.6: `
	out := extest.Stdout(t, main)
	if !strings.HasPrefix(out, want) || !strings.Contains(out, "frame g 401 at 1:20.0") ||
		!strings.Contains(out, "polled frame g 399\npolled frame g 400\npolled frame g 401\n") {
		t.Fatal(out)
	}
}
