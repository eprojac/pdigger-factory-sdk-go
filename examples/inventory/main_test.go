package main

import (
	"os"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
)

// The inventory of the fixture recording dry2-0582-0600 after its last frame (g 3009): its 192 events (the
// reference interpreter's count) and the 42 stones of that frame's world.
func TestInventory(t *testing.T) {
	extest.Args(t, "inventory", extest.Fixture("recordings", "dry2-0582-0600.ndjson.gz"))
	lines := strings.Split(strings.TrimSuffix(extest.Stdout(t, main), "\n"), "\n")
	golden, err := os.ReadFile(extest.Fixture("golden", "dry2-0582-0600.events.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(golden), "\n"); n != 192 {
		t.Fatalf("the golden file has %d events", n)
	}
	want := []string{
		"dry2 at 10:00.1, after 192 events: 42 stones",
		"stone 21: rect, frozen, at (-0.11, 0.67)",
		"stone 22: rect, frozen, at (0.17, 0.67)",
	}
	if len(lines) != 43 || strings.Join(lines[:3], "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
}
