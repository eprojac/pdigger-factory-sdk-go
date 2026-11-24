#!/bin/sh
# test.sh: the unit test run of Dockerfile.test (make test) and of CI. No network is needed: the tests use the
# fixtures and the in-process fake server only. Steps: gofmt and go vet in the three modules, the SDK's tests with
# the race detector and 100 % statement coverage (the golden tests included), then the examples (every code sample
# of the docs) and the picture generator in check mode.
set -eu
cd "$(dirname "$0")/.."
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  echo "gofmt -l: these files need gofmt -w:"
  echo "$unformatted"
  exit 1
fi
go vet ./...
(cd examples && go vet ./...)
(cd tools && go vet ./...)
go test -race -count=1 -timeout 30m -covermode=atomic -coverpkg=./... -coverprofile=cover.out -p 4 ./...
sh scripts/coverage-check.sh cover.out
(cd examples && go test -count=1 ./...)
(cd tools && go test -count=1 ./...)
echo "unit tests: pass (race detector on, every statement covered, golden outputs equal)"
