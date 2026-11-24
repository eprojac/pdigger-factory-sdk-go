package pdiggerfactory_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

// goldenNames lists every recording of fixtures/recordings (the golden files must exist for each).
func goldenNames(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("fixtures", "recordings", "*.ndjson*"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixture recordings: %v", err)
	}
	var names []string
	for _, p := range paths {
		base := filepath.Base(p)
		base = strings.TrimSuffix(base, ".gz")
		names = append(names, strings.TrimSuffix(base, ".ndjson"))
	}
	sort.Strings(names)
	return names
}

func recordingPath(t *testing.T, name string) string {
	t.Helper()
	for _, ext := range []string{".ndjson.gz", ".ndjson"} {
		p := filepath.Join("fixtures", "recordings", name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatalf("no recording %s", name)
	return ""
}

func readGolden(t *testing.T, name, suffix string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("fixtures", "golden", name+suffix))
	if err != nil {
		t.Fatalf("golden %s%s: %v", name, suffix, err)
	}
	return string(b)
}

func jsonValue(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("json: %v", err)
	}
	return v
}

// firstDiff shows the first differing line of two texts.
func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return "line " + itoa(i+1) + ":\n got  " + gl + "\n want " + wl
		}
	}
	return "equal"
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// TestGolden runs Replay over every fixture recording and compares the nine outputs with the reference's: events,
// the events' tray stones, activities, tray facts, the open accounts, the tray reports, the speckle answers, and the
// summary as text and as JSON; every tray's self-check is clean; Trays and Summary alone give what Run gives.
func TestGolden(t *testing.T) {
	for _, name := range goldenNames(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			r, err := pdiggerfactory.OpenReplay(ctx, recordingPath(t, name), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			res, err := r.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var ev, act strings.Builder
			for _, e := range res.Events {
				ev.WriteString(e.Describe() + "\n")
			}
			for _, a := range res.Activities {
				act.WriteString(a.Describe() + "\n")
			}
			if got, want := ev.String(), readGolden(t, name, ".events.txt"); got != want {
				t.Errorf("events differ: %s", firstDiff(got, want))
			}
			if got, want := act.String(), readGolden(t, name, ".activities.txt"); got != want {
				t.Errorf("activities differ: %s", firstDiff(got, want))
			}
			var ts strings.Builder // every event with a tray stone: the link before its frame, a revision's too
			for _, e := range res.Events {
				if e.TrayStone != "" {
					fmt.Fprintf(&ts, "%d %s %d %s\n", e.G, e.Type, *e.StoneID, e.TrayStone)
				}
			}
			if got, want := ts.String(), readGolden(t, name, ".tray-stones.txt"); got != want {
				t.Errorf("tray stones differ: %s", firstDiff(got, want))
			}
			var tf, kept strings.Builder
			for _, f := range res.TrayFacts {
				tf.WriteString(f.Describe() + "\n")
			}
			for _, tr := range res.Trays { // every tray is kept, with its facts in order
				for _, f := range tr.Facts {
					kept.WriteString(f.Describe() + "\n")
				}
			}
			if got, want := tf.String(), readGolden(t, name, ".trays.txt"); got != want || kept.String() != want {
				t.Errorf("tray facts differ: %s", firstDiff(got, want))
			}
			if got, want := pdiggerfactory.DescribeOpenTxt(res.TrayFacts), readGolden(t, name, ".open.txt"); got != want {
				t.Errorf("open accounts differ: %s", firstDiff(got, want))
			}
			for _, tr := range res.Trays { // every account is consistent
				if bad := tr.Check(); len(bad) != 0 {
					t.Errorf("tray %d breaks %v", tr.N, bad)
				}
			}
			var rep strings.Builder // one report per tray that left (section 4.7)
			for i := range res.Trays {
				if r := res.Trays[i].Report(); r != nil {
					rep.WriteString(r.Describe() + "\n")
				}
			}
			if got, want := rep.String(), readGolden(t, name, ".reports.txt"); got != want {
				t.Errorf("reports differ: %s", firstDiff(got, want))
			}
			speckle := pdiggerfactory.DescribeSpeckleTxt(res.Speckle) // every answer read (section 4.6.12)
			if got, want := speckle, readGolden(t, name, ".speckle.txt"); got != want {
				t.Errorf("speckle answers differ: %s", firstDiff(got, want))
			}
			if got, want := res.Summary.Describe(), readGolden(t, name, ".summary.txt"); got != want {
				t.Errorf("summary text differs: %s", firstDiff(got, want))
			}
			sj, err := json.Marshal(res.Summary)
			if err != nil {
				t.Fatal(err)
			}
			got, want := jsonValue(t, sj), jsonValue(t, []byte(readGolden(t, name, ".summary.json")))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("summary json differs:\n got  %s\n want %s", sj, readGolden(t, name, ".summary.json"))
			}
			trays, err := r.Trays(ctx)
			if err != nil || !reflect.DeepEqual(trays, res.Trays) {
				t.Errorf("Trays differs from Run's: %v", err)
			}
			reports, err := r.TrayReports(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var again strings.Builder
			for _, x := range reports {
				again.WriteString(x.Describe() + "\n")
			}
			if again.String() != rep.String() {
				t.Errorf("TrayReports differs from the trays' reports: %s", firstDiff(again.String(), rep.String()))
			}
			if s, err := r.Summary(ctx); err != nil || !reflect.DeepEqual(s, res.Summary) {
				t.Errorf("Summary differs from Run's: %v", err)
			}
		})
	}
}
