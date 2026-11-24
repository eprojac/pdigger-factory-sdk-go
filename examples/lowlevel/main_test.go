package main

import (
	"regexp"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// A fake unit that plays dry1-0296-0306-full (all five layers) and answers the render operations from the replies
// recorded on 20.20.
func TestLowLevel(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0296-0306-full.ndjson.gz")),
		factorytest.WithReplies(extest.Fixture("api")))
	out := extest.Stdout(t, main)
	want := regexp.MustCompile(`^API [0-9.]+, build \S+
frame g 1530 at 5:05\.1: 41 stones
tray map 40x64: \d+ of 2560 cells are tray
scene3d: \d+ stones, surface 40x64 with \d+ flat cells
$`)
	if !want.MatchString(out) {
		t.Fatal(out)
	}
}

// The fake unit as tools/fakeunit and the sample recording serve it: aug28-1270-1300 has the world and cards
// layers only, so its frames come without the raw layer and the program says so instead of failing.
func TestLowLevelWithoutRaw(t *testing.T) {
	for name, rec := range map[string]factorytest.Option{
		"fixture": factorytest.WithRecording(extest.Fixture("recordings", "aug28-1270-1300.ndjson.gz")),
		"sample":  factorytest.WithSampleRecording(),
	} {
		t.Run(name, func(t *testing.T) {
			extest.Unit(t, rec, factorytest.WithReplies(extest.Fixture("api")))
			out := extest.Stdout(t, main)
			want := regexp.MustCompile(`^API [0-9.]+, build \S+
frame g 6500 at 21:40\.0: \d+ stones
frame g 6500 has no raw layer, so no tray map
scene3d: \d+ stones, surface 40x64 with \d+ flat cells
$`)
			if !want.MatchString(out) {
				t.Fatal(out)
			}
		})
	}
}
