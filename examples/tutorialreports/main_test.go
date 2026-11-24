package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
)

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(extest.Fixture("golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Over the fixture dry1-0018-0153 the program prints the golden report, its row as JSON (the changes at least 0.9
// likely to be right, and every move) and, since the report is not complete, the tray's golden facts: what the other
// languages' tutorial programs print.
func TestTutorialReports(t *testing.T) {
	extest.Args(t, "tutorialreports", extest.Fixture("recordings", "dry1-0018-0153.ndjson.gz"))
	reports, facts := golden(t, "dry1-0018-0153.reports.txt"), golden(t, "dry1-0018-0153.trays.txt")
	lines := strings.SplitAfter(extest.Stdout(t, main), "\n")
	n := strings.Count(reports, "\n")
	if len(lines) < n+2 || strings.Join(lines[:n], "") != reports || strings.Join(lines[n+1:], "") != facts {
		t.Fatalf("got\n%s", strings.Join(lines, ""))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[n]), &got); err != nil {
		t.Fatal(err)
	}
	replay, err := pdiggerfactory.OpenReplay(context.Background(), extest.Fixture("recordings", "dry1-0018-0153.ndjson.gz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	report, err := replay.TrayReports(context.Background())
	if err != nil || len(report) != 1 || report[0].Arrived.Wall == nil || report[0].Left.Wall == nil {
		t.Fatal(report, err)
	}
	arrived := func(ks ...string) []any {
		out := []any{}
		for _, k := range ks {
			out = append(out, "20261122T023847.0Z-1-"+k)
		}
		return out
	}
	want := map[string]any{"tray": 1.0, "arrived": *report[0].Arrived.Wall, "left": *report[0].Left.Wall,
		"removed": arrived("117", "127", "126", "105", "112", "125"), // 0.98 each
		"added": []any{ // 0.96 each; 151, 246, 249 and 278 are 0.52
			"20261122T024104.0Z-1-177", "20261122T024018.8Z-1-193", "20261122T024029.7Z-1-201",
			"20261122T024031.0Z-1-203", "20261122T024118.1Z-1-279", "20261122T024121.3Z-1-280",
			"20261122T024121.3Z-1-281", "20261122T024124.2Z-1-282", "20261122T024126.9Z-1-284"},
		"moved":   arrived("85", "86", "88", "89", "90", "104", "118", "120"), // 0.87 each, every move kept
		"unclear": 18.0, "complete": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s", lines[n])
	}
}

// The default: the fixture dry1-0018-0153, as `go run ./tutorialreports` from the examples folder.
func TestTutorialReportsDefault(t *testing.T) {
	extest.Args(t, "tutorialreports")
	reports := golden(t, "dry1-0018-0153.reports.txt")
	if err := os.Chdir(".."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir("tutorialreports") })
	if out := extest.Stdout(t, main); !strings.HasPrefix(out, reports) {
		t.Fatal(out)
	}
}
