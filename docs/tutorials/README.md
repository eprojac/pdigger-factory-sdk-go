# Tutorials

Five short tutorials take you from simple mode, one report per tray, through advanced mode to drawing a frame by hand.
Every program in them is an example of this repository: the tests of `examples/` run each one against the fixture
recordings and the SDK's fake unit, and `docscheck` fails when a Go block of these pages is not, word for word, in one
of the examples.

| Tutorial | What you build |
| --- | --- |
| [Start here](00-start-here.md) | simple mode: one report per tray (the stones removed, added and moved, when and where, and whether that is all of it), the same reports as data, the details behind a report that is not complete, and the reports of a unit, live |
| [High level](01-high-level.md) | advanced mode: what the hands did, as sentences and as data: activities, trays and the stones taken off them, the summary, a stone's path; replayed offline, then followed live |
| [Low level](02-low-level.md) | advanced mode: one call per API operation: health and speed, the model, a stone and the focus, frames and the model's maps, draw lists, stones and hands drawn by hand, a frame's image data |
| [Live cameras](03-live-cameras.md) | advanced mode: a plant camera selected, warmed up and followed, its pictures, and what differs on a camera |
| [Go](04-go.md) | what is particular to this SDK: contexts and iterators, replays and the core, one error type, the fake unit in your tests |

## Before you start

- Go 1.23 or newer (the streams are range-over-func iterators). Until the first release is tagged, take the SDK from
  the repository's main branch:

  ```sh
  go get github.com/eprojac/pdigger-factory-sdk-go@main
  ```

- To run the tutorials' programs as they are, clone the repository and run them from its `examples` folder:
  `go run ./tutorialreports`.
- For the first two tutorials, nothing else: they replay recordings, offline, such as the fixture recordings in
  `fixtures/recordings/` or the published ones at https://factory.pdigger.com/developers/data/.
- For the live steps, a unit's address in `PDIGGER_FACTORY_URL`, which every example reads. Without a unit,
  `make fake-unit` (in the repository root, in another terminal) serves the SDK's fake one on http://127.0.0.1:8120,
  playing a fixture recording; give it the one the low-level tutorial shows with
  `make fake-unit ARGS="-recording ../fixtures/recordings/dry1-0296-0306-full.ndjson.gz"`, and add `-live` for a camera
  made from the recording.

The unit interprets one source at a time for everybody: a recording or a live camera, on one shared session. A
control (select, seek, play, pause, focus) changes what every viewer sees, so the tutorials send dry runs, which the
unit checks without applying them; only the live-camera tutorial really selects a camera.

The same tutorials, step for step, are in the other languages:
[Python](https://github.com/eprojac/pdigger-factory-sdk-python/tree/main/docs/tutorials),
[Rust](https://github.com/eprojac/pdigger-factory-sdk-rust/tree/main/docs/tutorials) and
[TypeScript](https://github.com/eprojac/pdigger-factory-sdk-ts/tree/main/docs/tutorials).
