package main

import (
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestSession(t *testing.T) {
	rec := factorytest.WithRecording(extest.Fixture("recordings", "joined-mid-move.ndjson.gz"))
	// a unit with a camera, which its session shows: the select is a dry run
	f := extest.Unit(t, rec, factorytest.WithLiveCamera())
	out := extest.Stdout(t, main)
	for _, want := range []string{"dry1        recording: Dry side · 1", "cam-live    live camera: Live camera",
		"first live camera: cam-live (FakeFactory); watching it would start session 1795314879 (dry run: nothing changed)",
		"session 1795314878 plays cam-live at 3.7 s (playing true)"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if s, _, _ := f.SessionState(); s != 1795314878 {
		t.Fatal("the dry run changed the session")
	}
	// a unit of a build before the cameras: recordings only
	extest.Unit(t, rec)
	if out := extest.Stdout(t, main); !strings.Contains(out, "this unit lists no camera: the shared session plays recordings") {
		t.Fatal(out)
	}
}
