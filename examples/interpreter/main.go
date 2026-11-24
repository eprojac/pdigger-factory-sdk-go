// Command interpreter shows the high level on a unit: it waits for the next lift, follows that stone until its move
// ends, then prints both hands and the summary of what it saw.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./interpreter
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx := context.Background()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	if err := follow(ctx, client); err != nil {
		log.Fatal(err)
	}
}

// follow waits for a lift and follows the lifted stone until its move ends.
func follow(ctx context.Context, client *pdiggerfactory.Client) error {
	interpreter := client.Interpreter()
	isLift := func(e pdiggerfactory.Event) bool { return e.Type == pdiggerfactory.EventLift && e.StoneID != nil }
	lift, err := interpreter.WaitFor(ctx, isLift, 10*time.Minute)
	if err != nil {
		return err
	}
	fmt.Println("lift: ", lift.Describe())
	stone := *lift.StoneID
	for update, err := range interpreter.Updates(ctx) { // goes on with the rest of the lift's frame
		if err != nil {
			return err
		}
		for _, event := range update.Events {
			if event.StoneID != nil && *event.StoneID == stone {
				fmt.Println("event:", event.Describe())
			}
		}
		for _, activity := range update.Activities {
			if activity.Kind == pdiggerfactory.KindMove && activity.StoneID != nil && *activity.StoneID == stone {
				fmt.Println("move: ", activity.Describe(), "->", activity.Outcome)
				hands := update.State.Hands
				fmt.Printf("hands: left %s, right %s\n", hands.Left.State, hands.Right.State)
				fmt.Print(interpreter.Summary().Describe())
				return nil
			}
		}
	}
	return ctx.Err()
}
