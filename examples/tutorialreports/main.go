// Command tutorialreports is the first tutorial (docs/tutorials/00-start-here.md) in one program, offline: one report
// per tray, the same reports as data, and the details behind a report that is not complete. Every iteration of a
// Replay reads the recording again from the start.
//
//	go run ./tutorialreports ../fixtures/recordings/dry1-0018-0153.ndjson.gz
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

// row is a report as one JSON line, with the changes likely to be right.
type row struct {
	Tray     int64    `json:"tray"`
	Arrived  *float64 `json:"arrived"` // UTC epoch seconds (null when the unit's clock is not known)
	Left     *float64 `json:"left"`
	Removed  []string `json:"removed"`
	Added    []string `json:"added"`
	Moved    []string `json:"moved"`
	Unclear  int64    `json:"unclear"`
	Complete bool     `json:"complete"`
}

const threshold = 0.9 // keep the changes at least this likely to be right; every move is kept

// stones lists the stones of the changes whose confidence (the probability that they are right) is at least floor.
func stones(changes []pdiggerfactory.TrayChange, floor float64) []string {
	out := []string{}
	for _, change := range changes {
		if change.Confidence >= floor {
			out = append(out, change.Stone)
		}
	}
	return out
}

func main() {
	ctx := context.Background()
	source := "../fixtures/recordings/dry1-0018-0153.ndjson.gz"
	if len(os.Args) > 1 {
		source = os.Args[1] // a path, or an http(s) URL such as a published recording
	}
	replay, err := pdiggerfactory.OpenReplay(ctx, source, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()

	// 1. One report per tray: the stones removed, added and moved, when and where, and whether that is all of it
	reports, err := replay.TrayReports(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, report := range reports {
		fmt.Println(report.Describe())
	}

	// 2. The same reports as data, one JSON line per tray, for a database, an MES or a spreadsheet
	for _, report := range reports {
		line, _ := json.Marshal(row{report.Tray, report.Arrived.Wall, report.Left.Wall,
			stones(report.Removed, threshold), stones(report.Added, threshold), stones(report.Moved, 0),
			report.Unclear, report.Complete})
		fmt.Println(string(line)) // json.Marshal(report) has every field
	}

	// 3. Behind a report that is not complete, advanced mode: the tray's facts, each with its reason
	trays, err := replay.Trays(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, tray := range trays {
		if report := tray.Report(); report == nil || report.Complete {
			continue
		}
		for _, fact := range tray.Facts {
			// 0:30.1 tray 1: stone 20261122T023847.0Z-1-94 is not sure any more (it was pushed)
			fmt.Println(fact.Describe())
		}
	}
}
