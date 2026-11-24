# The SDK's tasks. make test is what CI runs: the unit tests in Docker, with no network.
IMAGE ?= pdigger-factory-sdk-go-test
STATICCHECK ?= honnef.co/go/tools/cmd/staticcheck@2026.2.2

.PHONY: test test-local lint docs pictures fake-unit integration clean

## test: the unit tests in Docker with no network: race detector, 100 % statement coverage, golden outputs, examples
test:
	docker build -t $(IMAGE) -f Dockerfile.test .
	docker run --rm --network none $(IMAGE)

## test-local: the same with the local Go toolchain (1.23 or later)
test-local:
	sh scripts/test.sh

## lint: gofmt, go vet and staticcheck in the three modules (the root, examples/, tools/)
lint:
	@test -z "$$(gofmt -l .)" || { echo "gofmt -l:"; gofmt -l .; exit 1; }
	go vet ./... && (cd examples && go vet ./...) && (cd tools && go vet ./...)
	go run $(STATICCHECK) ./... && (cd examples && go run $(STATICCHECK) ./...) && (cd tools && go run $(STATICCHECK) ./...)

## docs: the API reference as text, from the doc comments, in api-reference/
docs:
	mkdir -p api-reference
	go doc -all . > api-reference/pdiggerfactory.txt
	go doc -all ./codec > api-reference/codec.txt
	go doc -all ./factorytest > api-reference/factorytest.txt

## pictures: redraws docs/img/quickstart-live.svg and docs/img/replay-quickstart.svg from the fixtures
pictures:
	cd tools && go run ./pictures

## fake-unit: serves the fake unit on http://127.0.0.1:8120 (ARGS="-live -recording ..." to change what it plays)
fake-unit:
	cd tools && go run ./fakeunit $(ARGS)

## integration: the opt-in suites whose variables are set (either or both): read-only checks against the unit at
## PDIGGER_FACTORY_URL (observing streams, dry-run controls), and replays of the complete recordings in the folder
## PDIGGER_FACTORY_RECORDINGS compared with their summaries
integration:
	@test -n "$(PDIGGER_FACTORY_URL)$(PDIGGER_FACTORY_RECORDINGS)" || { echo 'make integration: set PDIGGER_FACTORY_URL=http://<unit>:8120 and/or PDIGGER_FACTORY_RECORDINGS=<folder>' >&2; exit 2; }
	go test -run '^TestIntegration' -count=1 -timeout 2h -v .

## clean: removes the coverage profile and the API reference
clean:
	rm -f cover.out
	rm -rf api-reference
