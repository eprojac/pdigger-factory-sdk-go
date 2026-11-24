// Command errors shows how the SDK reports errors: one type, *pdiggerfactory.Error, with a stable Code to branch on,
// the HTTP status and the server's message and field; retries that happen before an error reaches you; dry runs that
// let the server validate a control; and the context's errors for deadlines and cancellation.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./errors
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

func main() {
	ctx := context.Background()
	if _, err := pdiggerfactory.New("factory-orin.local:8120"); err != nil {
		fmt.Println("a base URL needs its scheme:", pdiggerfactory.AsError(err).Code)
	}
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"), pdiggerfactory.WithRetries(2))
	if err != nil {
		log.Fatal(err)
	}

	// An API error: branch on the code, never on the message.
	_, err = client.API().GetFrame(ctx, pdiggerfactory.FrameAt(1), nil)
	var e *pdiggerfactory.Error
	if errors.As(err, &e) {
		fmt.Printf("frame 1: HTTP %d %s (field %s)\n", e.Status, e.Code, e.Field)
		if r := e.Retained; e.Code == pdiggerfactory.CodeNotRetained && r != nil && r.G0 != nil && r.G1 != nil {
			fmt.Printf("the unit holds frames %d to %d of session %d\n", *r.G0, *r.G1, r.Session)
		}
	}

	// A control the server refuses, checked with a dry run: the shared session does not move.
	_, err = client.Session().Seek(ctx, -5, pdiggerfactory.DryRun())
	fmt.Println("seek to -5 s:", pdiggerfactory.AsError(err).Code, "retryable:", pdiggerfactory.IsRetryable(err))

	// Deadlines and cancellation come back as the context's errors.
	_, err = client.Interpreter().WaitFor(ctx, pdiggerfactory.AboutStone(999999), 20*time.Millisecond)
	fmt.Println("no event about stone 999999 in time:", errors.Is(err, context.DeadlineExceeded))
}
