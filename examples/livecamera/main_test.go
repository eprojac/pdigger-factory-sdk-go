package main

import (
	"regexp"
	"strings"
	"testing"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestLiveCamera(t *testing.T) {
	// the fake unit runs in real time, so its camera connects and warms up as a unit's does (about 4 s)
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0296-0306-full.ndjson.gz")),
		factorytest.WithLiveCamera(), factorytest.WithClock(pdiggerfactory.SystemClock{}))
	extest.Args(t, "livecamera", "4")
	lines := strings.Split(strings.TrimSpace(extest.Stdout(t, main)), "\n")
	want := []string{
		`cam-live \(Live camera\) is live: session [0-9]+, 0\.25 s behind the camera`,
		`frame g 50: a [0-9]+-byte JPEG`,
	}
	for range 4 {
		want = append(want, `[0-9]{2}:[0-9]{2}:[0-9]{2} UTC  [0-9]+:[0-9]{2}\.[0-9] .+`)
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines, want %d:\n%s", len(lines), len(want), strings.Join(lines, "\n"))
	}
	for i, w := range want {
		if !regexp.MustCompile("^" + w + "$").MatchString(lines[i]) {
			t.Errorf("line %d: %q is not like %q", i+1, lines[i], w)
		}
	}
	// a camera given by its serial
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "dry1-0296-0306-full.ndjson.gz")),
		factorytest.WithLiveCamera(), factorytest.WithClock(pdiggerfactory.SystemClock{}))
	extest.Args(t, "livecamera", factorytest.LiveCamera, "1")
	if lines := strings.Split(strings.TrimSpace(extest.Stdout(t, main)), "\n"); len(lines) != 3 {
		t.Fatal(lines)
	}
}
