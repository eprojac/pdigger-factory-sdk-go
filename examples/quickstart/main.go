// Command quickstart follows the unit's shared session and prints each event as a sentence, until Ctrl-C, whatever the
// session shows: a recording, or one of the plant cameras live (./livecamera selects one). The program is the same.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstart
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
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL")) // e.g. http://factory-orin.local:8120
	if err != nil {
		log.Fatal(err)
	}
	for event, err := range client.Interpreter().Events(ctx) {
		if err != nil {
			log.Print(err) // a frame that did not decode (the stream goes on) or the error that ended it
			continue
		}
		fmt.Println(event.Describe()) // 21:17.8 Right hand lifts stone 2660
	}
}
