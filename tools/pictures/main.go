// Command pictures draws the README's pictures of the quickstarts' output from the fixture recordings, through the
// SDK itself: the live quickstart following a factorytest fake unit, and the replay quickstart over a recording
// file. Nothing is typed in by hand; the elided lines are counted on the picture.
//
//	go run ./pictures          # writes ../docs/img/quickstart-live.svg and ../docs/img/replay-quickstart.svg
//	go run ./pictures -check   # fails if the checked-in pictures differ from what the SDK prints now
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func main() {
	check := flag.Bool("check", false, "compare with the checked-in pictures instead of writing them")
	root := flag.String("root", "..", "the SDK's folder")
	flag.Parse()
	pics, err := pictures(context.Background(), *root)
	if err != nil {
		log.Fatal(err)
	}
	stale := false
	for name, svg := range pics {
		path := filepath.Join(*root, "docs", "img", name)
		if *check {
			old, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(old, svg) {
				fmt.Printf("%s is stale: run go run ./pictures in tools/\n", path)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(path, svg, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", path)
	}
	if stale {
		os.Exit(1)
	}
}

// pictures draws every picture, by file name.
func pictures(ctx context.Context, root string) (map[string][]byte, error) {
	live, err := liveQuickstart(ctx, root)
	if err != nil {
		return nil, err
	}
	replay, err := replayQuickstart(ctx, root)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"quickstart-live.svg": live, "replay-quickstart.svg": replay}, nil
}

// liveQuickstart follows a fake unit that plays aug28-1270-1300, as examples/quickstart does, until the place of
// stone 2660, and draws the lines it printed around the move.
func liveQuickstart(ctx context.Context, root string) ([]byte, error) {
	rec := filepath.Join(root, "fixtures", "recordings", "aug28-1270-1300.ndjson.gz")
	frames, err := span(ctx, rec)
	if err != nil {
		return nil, err
	}
	fake, err := factorytest.New(factorytest.WithRecording(rec))
	if err != nil {
		return nil, err
	}
	client, err := pdiggerfactory.New("http://factory-orin.local:8120", pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lines []string
	var place pdiggerfactory.Event
	for event, err := range client.Interpreter().Events(ctx) {
		if err != nil {
			return nil, err
		}
		lines = append(lines, event.Describe())
		if event.Type == pdiggerfactory.EventPlace && event.StoneID != nil && *event.StoneID == 2660 {
			place = event
			break
		}
	}
	keep := around(lines, 3, func(l string) bool { return strings.Contains(l, "stone 2660") && !strings.Contains(l, "pushes") })
	return terminal(terminalPicture{
		title:   "Quickstart: follow the camera",
		command: "PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstart",
		lines:   lines,
		keep:    keep,
		mark:    func(l string) bool { return strings.Contains(l, "stone 2660") },
		end:     "^C",
		caption: wrap(fmt.Sprintf("Real output of examples/quickstart following a factorytest fake unit whose shared "+
			"session plays the fixture recording aug28-1270-1300 (%s), stopped after the place of stone 2660 at %s "+
			"(g %d). Highlighted: the move of stone 2660. Drawn by tools/pictures from what the SDK prints.",
			frames, pdiggerfactory.FormatClock(place.T), place.G), captionWidth),
	}), nil
}

// replayQuickstart runs examples/replay's program over dry1-0296-0306-full and draws its output around the lift and
// place of stone 643, with the whole summary.
func replayQuickstart(ctx context.Context, root string) ([]byte, error) {
	rec := filepath.Join(root, "fixtures", "recordings", "dry1-0296-0306-full.ndjson.gz")
	frames, err := span(ctx, rec)
	if err != nil {
		return nil, err
	}
	replay, err := pdiggerfactory.OpenReplay(ctx, rec, nil)
	if err != nil {
		return nil, err
	}
	defer replay.Close()
	result, err := replay.Run(ctx)
	if err != nil {
		return nil, err
	}
	var lines []string
	var lift, place pdiggerfactory.Event
	for _, event := range result.Events {
		lines = append(lines, event.Describe())
		if event.StoneID != nil && *event.StoneID == 643 {
			switch event.Type {
			case pdiggerfactory.EventLift:
				lift = event
			case pdiggerfactory.EventPlace:
				place = event
			}
		}
	}
	n := len(lines)
	lines = append(lines, strings.Split(strings.TrimSuffix(result.Summary.Describe(), "\n"), "\n")...)
	keep := around(lines[:n], 2, func(l string) bool { return strings.Contains(l, "stone 643") })
	return terminal(terminalPicture{
		title:   "Quickstart: replay a recording",
		command: "go run ./replay ../fixtures/recordings/dry1-0296-0306-full.ndjson.gz",
		lines:   lines,
		keep:    func(i int) bool { return i >= n || keep(i) },
		mark:    func(l string) bool { return strings.Contains(l, "stone 643") },
		caption: wrap(fmt.Sprintf("Real output of examples/replay over the fixture recording dry1-0296-0306-full (%s, "+
			"all five layers): the events around the lift and place of stone 643 (%s to %s), then the summary of the "+
			"excerpt. Drawn by tools/pictures from what the SDK prints.", frames, pdiggerfactory.FormatClock(lift.T),
			pdiggerfactory.FormatClock(place.T)), captionWidth),
	}), nil
}

// span is what the captions say of a recording, read from its frames: its video, the media times of its first and
// last frame and their g, as in "video dry1, 4:55.2 to 5:05.1, frames g 1480 to 1530".
func span(ctx context.Context, rec string) (string, error) {
	replay, err := pdiggerfactory.OpenReplay(ctx, rec, nil)
	if err != nil {
		return "", err
	}
	defer replay.Close()
	var first, last *pdiggerfactory.Frame
	for frame, err := range replay.Frames(ctx) {
		if err != nil {
			return "", err
		}
		if first == nil {
			first = frame
		}
		last = frame
	}
	if first == nil {
		return "", fmt.Errorf("%s has no frames", rec)
	}
	return fmt.Sprintf("video %s, %s to %s, frames g %d to %d", first.Video, pdiggerfactory.FormatClock(first.T),
		pdiggerfactory.FormatClock(last.T), first.G, last.G), nil
}

// captionWidth is the longest caption line, in characters.
const captionWidth = 116

// wrap breaks a text into lines of at most width characters, at spaces.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// around keeps the first lines, the last line and ctx lines around every line that matches.
func around(lines []string, ctx int, match func(string) bool) func(int) bool {
	keep := map[int]bool{0: true, 1: true, 2: true, len(lines) - 1: true}
	for i, l := range lines {
		if match(l) {
			for j := i - ctx; j <= i+ctx; j++ {
				keep[j] = true
			}
		}
	}
	return func(i int) bool { return keep[i] }
}
