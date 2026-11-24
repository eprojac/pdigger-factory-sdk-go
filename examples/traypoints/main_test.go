package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// The tray points of dry1-0018-0153, a busy tray: its window, the stones removed for certain, and the two dry-run
// seeks, which leave the shared session where it was.
func TestTrayPoints(t *testing.T) {
	path := extest.Fixture("recordings", "dry1-0018-0153.ndjson.gz")
	f := extest.Unit(t, factorytest.WithRecording(path))
	before, _, _ := f.SessionState()
	extest.Args(t, "traypoints", path, "1")
	got := strings.Split(strings.TrimSuffix(extest.Stdout(t, main), "\n"), "\n")
	want := []string{"tray 1: arrived 0:22.5, leaving 2:24.0"}
	for _, n := range []int{105, 112, 117, 125, 126, 127} {
		want = append(want, fmt.Sprintf("  removed: 20261122T023847.0Z-1-%d", n))
	}
	want = append(want, "seek to 0:22.5 would show dry1 from 0:22.5", "seek to 2:24.0 would show dry1 from 2:24.0")
	if !slices.Equal(got, want) {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	if after, _, _ := f.SessionState(); after != before {
		t.Fatal("a dry run changed the session")
	}
}
