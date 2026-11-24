// Package liftcount is an example of code built on the SDK, with its unit test (liftcount_test.go) written against
// the SDK's fake server: no unit, no network, no real clock.
package liftcount

import (
	"context"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

// Lifts follows the unit's session until n stones were lifted and returns the sentences of those lifts.
func Lifts(ctx context.Context, client *pdiggerfactory.Client, n int) ([]string, error) {
	var lifts []string
	for event, err := range client.Interpreter().Events(ctx) {
		if err != nil {
			return lifts, err
		}
		if event.Type == pdiggerfactory.EventLift {
			lifts = append(lifts, event.Describe())
			if len(lifts) == n {
				break
			}
		}
	}
	return lifts, nil
}
