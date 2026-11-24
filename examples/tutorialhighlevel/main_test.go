package main

import (
	"os"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
)

func golden(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(extest.Fixture("golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// Over the fixture dry1-0018-0153 the program prints the golden activities, the nine moves as data, the golden tray
// facts, the tray's points, the golden summary and stone 155's path: what the other languages' tutorial programs print.
func TestTutorialHighLevel(t *testing.T) {
	extest.Args(t, "tutorialhighlevel", extest.Fixture("recordings", "dry1-0018-0153.ndjson.gz"), "155")
	var want []string
	want = append(want, golden(t, "dry1-0018-0153.activities.txt")...)
	want = append(want,
		"0:36.3 stone 117: right hand, placed, 0.2 s",
		"0:50.5 stone 128: left hand, dropped, 0.2 s",
		"1:09.2 stone 102: right hand, placed, 6.0 s",
		"1:18.8 stone 95: right hand, placed, 1.0 s",
		"1:19.2 stone 155: left hand, carried_away, 1.0 s",
		"1:25.6 stone 106: right hand, carried_away, 2.6 s",
		"1:30.3 stone 94: right hand, placed, 0.2 s",
		"2:01.3 stone 250: left hand, dropped, 1.6 s",
		"2:07.8 stone 102: right hand, placed, 0.2 s")
	want = append(want, golden(t, "dry1-0018-0153.trays.txt")...)
	want = append(want, "tray 1: 0:22.5 to 2:24.0, 6 stones removed for certain")
	want = append(want, golden(t, "dry1-0018-0153.summary.txt")...)
	want = append(want, "stone 155: 5 points from 1:15.6 to 1:19.8")
	got := strings.Split(strings.TrimSuffix(extest.Stdout(t, main), "\n"), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The defaults: the fixture recording and stone 155, as `go run ./tutorialhighlevel` from the examples folder.
func TestTutorialHighLevelDefaults(t *testing.T) {
	extest.Args(t, "tutorialhighlevel")
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir("tutorialhighlevel") })
	out := extest.Stdout(t, main)
	if !strings.HasSuffix(out, "stone 155: 5 points from 1:15.6 to 1:19.8\n") {
		t.Fatal(out)
	}
}
