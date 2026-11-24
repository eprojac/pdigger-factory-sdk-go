package main

import (
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// The raw packets of the fixture recording dry1-0296-0306-full, which has all five layers.
func TestRawPackets(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0296-0306-full.ndjson.gz")))
	want := `g 1480: 40 stones detected, the model took 241.0 ms (encoder 163.3 ms)
g 1481: 40 stones detected, the model took 241.2 ms (encoder 163.4 ms)
g 1482: 40 stones detected, the model took 241.1 ms (encoder 163.3 ms)
g 1483: 38 stones detected, the model took 241.1 ms (encoder 163.3 ms)
g 1484: 38 stones detected, the model took 241.1 ms (encoder 163.4 ms)
`
	if got := extest.Stdout(t, main); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
