package main

import (
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/examples/internal/extest"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestErrors(t *testing.T) {
	f := extest.Unit(t, factorytest.WithRecording(extest.Fixture("recordings", "joined-mid-move.ndjson.gz")))
	out := extest.Stdout(t, main)
	want := `a base URL needs its scheme: invalid_param
frame 1: HTTP 404 not_retained (field g)
the unit holds frames 399 to 420 of session 1795314878
seek to -5 s: time_out_of_range retryable: false
no event about stone 999999 in time: true
`
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if s, _, _ := f.SessionState(); s != 1795314878 {
		t.Fatal("the dry run moved the session")
	}
}
