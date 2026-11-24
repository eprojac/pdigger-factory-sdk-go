// Command quickstartreports is simple mode: one report per tray, which stones were removed, which were added and
// which moved, when and where on the tray, and whether that is the tray's whole account. A recording needs no unit;
// with -follow the same report comes from a unit as each tray leaves.
//
//	go run ./quickstartreports ../fixtures/recordings/dry1-0018-0153.ndjson.gz
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstartreports -follow
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt) // Ctrl-C ends the loop
	defer stop()
	source := "../fixtures/recordings/dry1-0018-0153.ndjson.gz"
	if len(os.Args) > 1 {
		source = os.Args[1] // a path, an http(s) URL such as a published recording, or -follow
	}
	if source != "-follow" {
		replay, err := pdiggerfactory.OpenReplay(ctx, source, nil)
		if err != nil {
			log.Fatal(err)
		}
		defer replay.Close()
		reports, err := replay.TrayReports(ctx)
		if err != nil {
			log.Fatal(err)
		}
		for _, report := range reports {
			fmt.Println(report.Describe())
		}
		return
	}
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL")) // e.g. http://factory-orin.local:8120
	if err != nil {
		log.Fatal(err)
	}
	for report, err := range client.Interpreter().TrayReports(ctx) { // one as each tray leaves
		if err != nil {
			log.Print(err) // a frame that did not decode (the stream goes on) or the error that ended it
			continue
		}
		fmt.Println(report.Describe()) // json.Marshal(report) is the same as data
	}
}
