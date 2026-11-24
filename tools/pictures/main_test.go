package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPicturesAreCurrent fails when a checked-in picture is not what the SDK prints now (go run ./pictures fixes it).
func TestPicturesAreCurrent(t *testing.T) {
	pics, err := pictures(context.Background(), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(pics) != 2 {
		t.Fatal(len(pics))
	}
	for name, svg := range pics {
		old, err := os.ReadFile(filepath.Join("..", "..", "docs", "img", name))
		if err != nil || !bytes.Equal(old, svg) {
			t.Errorf("docs/img/%s is stale: run go run ./pictures in tools/ (%v)", name, err)
		}
		if !bytes.HasPrefix(svg, []byte("<svg ")) || !strings.Contains(string(svg), "Drawn by tools/pictures") {
			t.Errorf("%s: not a picture", name)
		}
	}
}

// TestCaptionsGiveTheFrames checks the spans the captions give, as the recordings' frames have them, and that the
// README's captions of the pictures say the same.
func TestCaptionsGiveTheFrames(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(readme)), " ")
	for rec, want := range map[string]string{
		"aug28-1270-1300":     "video aug28, 21:10.0 to 21:40.0, frames g 6350 to 6500",
		"dry1-0296-0306-full": "video dry1, 4:55.2 to 5:05.1, frames g 1480 to 1530",
	} {
		got, err := span(context.Background(), filepath.Join("..", "..", "fixtures", "recordings", rec+".ndjson.gz"))
		if err != nil || got != want {
			t.Fatalf("%s: %q, %v", rec, got, err)
		}
		if !strings.Contains(text, rec+" ("+got) {
			t.Errorf("README.md does not caption %s as (%s", rec, got)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty.ndjson")
	if err := os.WriteFile(empty, []byte(`{"recording":"pfr1","name":"empty","video":"dry1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := span(context.Background(), empty); err == nil || !strings.Contains(err.Error(), "no frames") {
		t.Fatal(err)
	}
	if _, err := span(context.Background(), filepath.Join(t.TempDir(), "none.ndjson")); err == nil {
		t.Fatal("a missing file has a span")
	}
}

func TestWrap(t *testing.T) {
	got := wrap("aa bb  cc\ndd eeeeeeeeee", 5)
	want := []string{"aa bb", "cc dd", "eeeeeeeeee"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q", got)
	}
	if wrap("", 5) != nil {
		t.Fatal("an empty text has lines")
	}
}

func TestTerminalElides(t *testing.T) {
	svg := string(terminal(terminalPicture{title: "t", command: "c", lines: []string{"a", "b", "c", "d", "<e>"},
		keep: func(i int) bool { return i == 0 || i == 4 }, mark: func(l string) bool { return l == "a" }, end: "^C"}))
	for _, want := range []string{`class="mark" x="48" y="130">a<`, "… 3 lines left out …", "&lt;e&gt;", ">^C<"} {
		if !strings.Contains(svg, want) {
			t.Errorf("no %q in %s", want, svg)
		}
	}
}
