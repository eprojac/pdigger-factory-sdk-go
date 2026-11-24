# Tutorials

Start with a tray report. The later tutorials show how to read the events and frames behind it.

| Tutorial | What you’ll do |
| --- | --- |
| [Read a tray report](00-start-here.md) | Read the changes, export a row and investigate uncertainty. |
| [Follow the interpreter](01-high-level.md) | Follow hand movements, get tray facts and trace a stone. |
| [Read individual frames](02-low-level.md) | Inspect model maps, draw stones and hands, and read a frame image. |
| [Watch a live camera](03-live-cameras.md) | Select a camera, wait for warm-up and read its pictures. |
| [Use the Go SDK](04-go.md) | Cancel iterators with a context and handle typed errors. |

## Before you start

Follow the [installation steps](../../README.md#install) to get the SDK and its example recordings. Run the commands
below in the `examples` directory of the clone.

The replay steps use the bundled files in `fixtures/recordings/`. You can also
[download longer recordings](https://factory.pdigger.com/developers/data/).

To try the live steps locally, run `make fake-unit` in a second terminal at the repository root and set
`PDIGGER_FACTORY_URL=http://127.0.0.1:8120` in the example terminal. For the low-level tutorial use
`make fake-unit ARGS="-recording ../fixtures/recordings/dry1-0296-0306-full.ndjson.gz"`. Add `-live` to the
arguments to simulate a camera.

A unit has one shared session. Selecting a camera, seeking or changing playback affects everyone watching. The
live-camera tutorial selects a real source. The other control examples use dry runs.

The same tutorials are available in
[Python](https://github.com/eprojac/pdigger-factory-sdk-python/tree/main/docs/tutorials) ·
[Rust](https://github.com/eprojac/pdigger-factory-sdk-rust/tree/main/docs/tutorials) ·
[TypeScript](https://github.com/eprojac/pdigger-factory-sdk-ts/tree/main/docs/tutorials).
