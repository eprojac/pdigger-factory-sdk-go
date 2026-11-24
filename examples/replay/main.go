// Command replay runs the interpreter over a recording, offline: a published recording (read as a stream, never
// loaded whole) or a local file. It prints every event, then the summary.
//
//	go run ./replay                                                    # the published dry1 recording
//	go run ./replay ../fixtures/recordings/aug28-1270-1300.ndjson.gz   # a local file
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	source := "https://factory.pdigger.com/developers/data/dry1.world.ndjson.gz"
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, source, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	result, err := replay.Run(ctx) // one pass: events, activities and the summary
	if err != nil {
		log.Fatal(err)
	}
	for _, event := range result.Events {
		fmt.Println(event.Describe()) // 5:02.8 Right hand places stone 643
	}
	fmt.Print(result.Summary.Describe())
}
