package main

import (
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// Over aug28-1270-1300 the next lift is stone 2660's at 21:17.8; its move ends with the place at 21:21.8. The loop
// after WaitFor goes on with the rest of the lift's frame, so the tip of that same frame comes next.
func TestInterpreter(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "aug28-1270-1300.ndjson.gz")))
	out := extest.Stdout(t, main)
	for _, want := range []string{
		"lift:  21:17.8 Right hand lifts stone 2660\nevent: 21:17.8 Right hand tips stone 2660 up on its edge\n" +
			"event: 21:18.0 Right hand carries stone 2660\nevent: 21:21.8 Right hand places stone 2660\n",
		"move:  21:17.8 Right hand moves stone 2660 0.84 units in 4.0 s -> placed\n",
		"hands: left ",
		"frames: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}
