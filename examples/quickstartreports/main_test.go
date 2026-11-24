package main

import (
	"os"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(extest.Fixture("golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Over a recording the program prints its golden reports, one per tray.
func TestQuickstartReports(t *testing.T) {
	extest.Args(t, "quickstartreports", extest.Fixture("recordings", "trays-synthetic.ndjson.gz"))
	if got, want := extest.Stdout(t, main), golden(t, "trays-synthetic.reports.txt"); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// The default: the fixture dry1-0018-0153, as `go run ./quickstartreports` from the examples folder.
func TestQuickstartReportsDefault(t *testing.T) {
	extest.Args(t, "quickstartreports")
	want := golden(t, "dry1-0018-0153.reports.txt")
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir("quickstartreports") })
	if got := extest.Stdout(t, main); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// With -follow it follows a fake unit whose session plays trays-synthetic: the report of each tray as it leaves (when
// the next one arrives), until it is interrupted.
func TestQuickstartReportsFollow(t *testing.T) {
	extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "trays-synthetic.ndjson.gz")))
	extest.Args(t, "quickstartreports", "-follow")
	want := strings.Split(golden(t, "trays-synthetic.reports.txt"), "\n")[:6] // trays 1, 2 and 3
	got := extest.Lines(t, main, len(want), extest.Interrupt)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
