# pDigger Factory SDK for Go

[![CI](https://github.com/eprojac/pdigger-factory-sdk-go/actions/workflows/ci.yml/badge.svg)](https://github.com/eprojac/pdigger-factory-sdk-go/actions/workflows/ci.yml)

Read tray reports from a pDigger Factory camera unit or replay a recording in Go. The `pdiggerfactory` package uses
contexts and Go iterators to follow a live unit.

Simple mode gives you one report per tray. It lists the stones added, removed or moved and flags uncertain changes.
Advanced mode gives you the events, frames and tracking evidence behind each report.

## Install

The SDK uses the Go standard library and requires Go 1.23 or later. Add it to an existing Go module with `go get`.

```sh
go get github.com/eprojac/pdigger-factory-sdk-go@v1.0.0
```

For package documentation, run `go doc github.com/eprojac/pdigger-factory-sdk-go` in your module or read the
[HTTP API guide](docs/low-level.md).

To try the examples, clone the repository and enter the examples module. Run the quickstart commands below from this
directory.

```sh
git clone https://github.com/eprojac/pdigger-factory-sdk-go
cd pdigger-factory-sdk-go/examples
```

<a id="simple-mode-one-report-per-tray"></a>

## Read a tray report

The repository includes a short recording of a worker sorting stones on a tray. Run the report example on that file.

```sh
go run ./quickstartreports ../fixtures/recordings/dry1-0018-0153.ndjson.gz
```

In Go, open the file or URL with `pdiggerfactory.OpenReplay(ctx, source, nil)`. After checking the error and
deferring `replay.Close()`, read the reports.

```go
reports, err := replay.TrayReports(ctx)
if err != nil {
	log.Fatal(err)
}
for _, report := range reports {
	fmt.Println(report.Describe())
}
```

The first two lines for `dry1-0018-0153` show the tray totals and the first removal.

```text
tray 1 (2026-11-22T02:38:47.0Z), 0:22.5 to 2:24.0: 11 removed, 13 added, 8 moved, not complete: 18 unclear, 1 not sure
  0:45.5 removed 20261122T023847.0Z-1-117 from (0.68, -1.06) (0.98)
```

The SDK marked six of the 11 removals as certain and five as unclear. The number at the end of each change is its
confidence score. [Walk through this report](docs/tutorials/00-start-here.md).

To replay a longer recording, pass its path or URL to the same command.
[Browse the published recordings](https://factory.pdigger.com/developers/data/).

## Follow a live unit

Use the same example with your unit’s address to print a report as each tray leaves the camera.

```sh
PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstartreports -follow
```

Use `json.Marshal(report)` to get the fields for a database or spreadsheet.
[Report example source](examples/quickstartreports/main.go).

<a id="advanced-mode-follow-the-unit"></a>
<a id="advanced-mode-replay-a-recording"></a>

## Read individual events

The interpreter can also tell you when a hand lifts a stone, which stones it pushes and where it places them. The
live example prints events from whichever source the unit is watching.

```sh
PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstart
```

With a client and context, range over the event stream.

```go
for event, err := range client.Interpreter().Events(ctx) {
	if err != nil {
		log.Print(err) // a frame that did not decode (the stream goes on) or the error that ended it
		continue
	}
	fmt.Println(event.Describe()) // 21:17.8 Right hand lifts stone 2660
}
```

To inspect events from a recording, pass its URL to the replay example.

```sh
go run ./replay https://factory.pdigger.com/developers/data/dry1.world.ndjson.gz
```

[Live event example](examples/quickstart/main.go) · [Replay example](examples/replay/main.go)

![Live events showing a hand lifting and placing stone 2660](docs/img/quickstart-live.svg)

The fake unit is playing aug28-1270-1300 (video aug28, 21:10.0 to 21:40.0, frames g 6350 to 6500). The last event
shown is the placement of stone 2660 at 21:21.8.

<details>
<summary>See the replay output</summary>

![Replay events showing stone 643 being lifted and placed](docs/img/replay-quickstart.svg)

The replay uses dry1-0296-0306-full (video dry1, 4:55.2 to 5:05.1, frames g 1480 to 1530). It prints the events
for stone 643 and ends with a summary of the recording excerpt.

</details>

<a id="advanced-mode-in-three-layers"></a>
<a id="high-level-the-interpreter"></a>
<a id="mid-level-streams"></a>
<a id="low-level-one-method-per-operation"></a>

## Choose an API

| Task | SDK entry point | Example | Guide |
| --- | --- | --- | --- |
| Follow hand movements and stone events | `client.Interpreter().Activities(ctx)` | [Source](examples/interpreter/main.go) | [Interpreter](docs/interpreter.md) |
| Read individual frames and late layers | `client.Streams().Frames` | [Source](examples/streams/main.go) | [Streams](docs/streams.md) |
| Request a frame, map or draw list | `client.API().GetFrame` | [Source](examples/lowlevel/main.go) | [HTTP API](docs/low-level.md) |

<a id="trays-ids-never-reused-facts-never-revised"></a>

Each tray stone has a stable ID, even if the camera tracker loses it and assigns another ID.
[How the SDK tracks tray stones](docs/interpreter.md#trays-and-tray-stones).

<a id="live-cameras-and-the-shared-session"></a>

Selecting a camera or seeking a recording changes the source or playhead for everyone watching that unit.
[Live camera tutorial](docs/tutorials/03-live-cameras.md).

## Errors

Use `errors.As` with `*pdiggerfactory.Error` to handle SDK failures. Branch on the stable error code when deciding
whether to retry or choose another frame. [Error handling and retries](docs/low-level.md).

## Async

Pass a `context.Context` to each operation. Cancel the context to interrupt a pending read, or break out of a
stream’s `range` loop to close the connection.

## Testing your code

`factorytest` serves recorded responses through an in-process transport or a loopback HTTP server.
`WithSampleRecording()` loads its embedded recording. [Testing guide](docs/testing.md).

## Docs

- [Work through the tutorials](docs/tutorials/README.md)
- [Understand sessions, coordinates and frame layers](docs/concepts.md)
- [Open and replay recordings](docs/replay.md)
- [Look up camera timings on the 20.20 unit](docs/BEHAVIOUR-20.20.md)
- [Browse the HTTP API](https://factory.pdigger.com/developers/api/) · [OpenAPI specification](spec/openapi.yaml)

Build the Go API reference with `make docs` from the repository root.

![How camera frames reach an application through the factory unit and SDK](docs/img/architecture.png)

## The other languages

All four SDKs produce the same report text for a given recording. [Shared SDK contract](spec/SDK-DESIGN.md).

| SDK | Reports, events, streams and replay | Viewer |
| --- | --- | --- |
| [Go](https://github.com/eprojac/pdigger-factory-sdk-go) | Yes | — |
| [Python](https://github.com/eprojac/pdigger-factory-sdk-python) | Yes | — |
| [Rust](https://github.com/eprojac/pdigger-factory-sdk-rust) | Yes | egui |
| [TypeScript](https://github.com/eprojac/pdigger-factory-sdk-ts) | Yes | three.js |

## Development

Run `make test` from the repository root to check the SDK in Docker. [Test instructions](TESTING.md) ·
[Contributing](CONTRIBUTING.md) · [Release instructions](RELEASING.md)

## Licence

[Apache License 2.0](LICENSE) · [Third-party notices](NOTICE)
