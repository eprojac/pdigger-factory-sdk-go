// Command activities follows the unit's shared session at the scale people talk about: each move of a stone (from
// the lift to where it ends up) and each burst of pushes, one sentence as each completes. It logs the stream's
// reconnects and stops on Ctrl-C.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./activities
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	interpreter := client.Interpreter(pdiggerfactory.OnReconnect(func(attempt int, err error) {
		log.Printf("reconnecting (attempt %d): %v", attempt, err) // the stream resumes where it stopped
	}))
	for activity, err := range interpreter.Activities(ctx) {
		if err != nil {
			log.Print(err)
			continue
		}
		fmt.Println(activity.Describe()) // 0:20.5 Both hands push stones 86, 90 and 94 (6 pushes in 0.6 s)
	}
}
