# Replays

Pass a file path or URL to `OpenReplay`. The interpreter reads recorded frames and side events in order, so you can
reproduce a tray report without connecting to the factory.

## Recordings

A recording is NDJSON, optionally gzipped, in the format `pfr1`: a header line, then every frame exactly as the API
sent it (late layers merged in) and the stream's side events (`clock`, `worker`, `gap`, `reset`) in the order they
came. The format is in [spec/SDK-DESIGN.md](../spec/SDK-DESIGN.md#6-recording-format-pfr1).

**Published recordings.** https://factory.pdigger.com/developers/data/ has four complete recordings (aug28, jul30,
dry1 and dry2, each from its first frame to its end: dry1 recorded on the 20.20 unit, the other three in a replay of
the same model and build on a Mac, as each header's `recorded_from` says), as `<video>.world.ndjson.gz` (the world and
cards layers, enough for the interpreter) and `<video>.full.ndjson.gz` (all five layers), with their sizes, sha256 and
golden summaries in `recordings.json`.

**Fixtures.** [fixtures/recordings/](../fixtures/recordings) has short excerpts, of the same recordings for dry1 and
aug28 and of other recordings made on the unit for dry2 (`dry2-pause-seek` with build `claude-20261122-hr4x1`, and
`dry2-0582-0600` kept from build `claude-20261114-int17` for its 13 frames without a world), as each header says,
and [fixtures/golden/](../fixtures/golden) what the maintainers' reference interpreter (not published) makes of
each. The SDK's golden tests replay every excerpt and compare.

Both are redacted: face keypoints are null, and appearance masks and depth textures are removed (the header's
`redacted` lists them). The interpreter needs none of them.

## Opening and iterating

`OpenReplay(ctx, source, opts)` takes a local `.ndjson` or `.ndjson.gz` path or an http(s) URL. A URL is read as a
stream, never loaded whole, and a gzip file is recognised by its first bytes. It reads the header (`replay.Header`:
the first line that is not blank, when it has `recording` and decodes, without a `source` that does not decode; nil
for a file without one); `Close` releases the file.

```go
func main() {
	source := "https://factory.pdigger.com/developers/data/dry1.world.ndjson.gz"
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	ctx := context.Background()
	replay, err := pdiggerfactory.OpenReplay(ctx, source, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer replay.Close()
	result, err := replay.Run(ctx) // one pass: events, activities and the summary
	if err != nil {
		log.Fatal(err)
	}
	for _, event := range result.Events {
		fmt.Println(event.Describe()) // 5:02.8 Right hand places stone 643
	}
	fmt.Print(result.Summary.Describe())
}
```

(from [examples/replay](../examples/replay/main.go))

| Method | What |
|---|---|
| `Run(ctx)` | one pass: all `Events`, all `Activities`, all `TrayFacts`, every tray (`Trays`) and the `Summary` |
| `Summary(ctx)` | one pass, the summary only |
| `Updates(ctx)` | an `*Update` per frame, then a final one (`Final`, `Frame` nil: the activities still open, closed, and the facts of the last tray leaving) |
| `Events(ctx)`, `Activities(ctx)` | just the events, or the activities (ending with the ones still open at the end) |
| `TrayFacts(ctx)`, `Trays(ctx)` | just the tray facts, or every tray of the recording ([trays](#trays)) |
| `Frames(ctx)` | the frames, without the interpreter and without the side lines (feed them to an `InterpreterCore` yourself) |
| `Items(ctx)` | the frames and the side events as stream items, as `Streams.Frames` would yield them |
| `Skipped()` | how many lines of a damaged file the iterations so far skipped as not decoding (see below) |

Every iteration reads the recording from the start with a new interpreter, so two loops over one replay give the same
output.

**Contexts and errors.** Each iteration reads under the context it is given: cancelling it ends the iteration at once,
even in a read that waits on a stalled connection, and closes the connection. The context of `OpenReplay` bounds only
the opening and the header, so a timeout given to `OpenReplay` does not cut a long replay short. Errors are
`*pdiggerfactory.Error`s: `transport` when the recording cannot be opened or read (the file or the connection failed,
or the context ended, which is not retryable), `not_found` for a URL that answers 404 (another error status keeps its
`Status`, with the API's code when the reply is an API error, else `decode`), and `decode` for bytes that are not a
recording (a gzip stream that is corrupt or cut short). The clock lines of the recording give the events and
activities their wall times, and its `gap` and `reset` lines (frames the stream dropped) end the trays' presence
questions open then. `Frames` gives the frames only, without those side lines, so to feed a core of your own, range
over `Items` and give it the `ClockItem`s (`SetClock`) and the `GapItem`s and `ResetItem`s (`Gap`) as well as the
frames. A recording that spans several sessions (a select or seek while it was made) is interpreted as the unit would:
moves open at a session change end as `interrupted`, and the open tray leaves.

**Damaged lines.** Each line is read by its keys (section 6 of the SDK design), with bytes that are not UTF-8 read as
U+FFFD: a line with `sse` is a side line and one with `g` and `cursor` a frame, and each must decode as the stream
event it was (2.3 rule 9: its fields there, not null, of their types; a side line's `sse` a string and `recv` a number
when it is there). A replay skips the lines it does not understand. It counts in `Skipped` those that do not decode:
lines that are not a JSON object (not JSON, `NaN` included, or JSON of another type), a first line with `recording`
that is not a header that decodes by its table (never a frame; a member the header cannot hold is left out of it, as
is a `source` that is no catalog entry), frame lines that do not decode, and side lines whose `sse` is not a string,
whose `recv` is not a number or whose data does not decode as the event the SDK knows. It ignores the others without
counting them: blank lines, objects with neither `sse` nor both `g` and `cursor` (a line with `recording` after the
first, say), and side lines of an event the SDK does not know. [fixtures/damaged](../fixtures/damaged) holds a line of
each kind, and the tests replay it.

## Trays

`Trays(ctx)` reads the full recording and returns its trays in order. Each tray includes its stones, facts and the
points when it settled and left. Use these points to seek to the same tray in a player.
[Tray identity and events](interpreter.md#trays-and-tray-stones).

```go
	trays, err := replay.Trays(ctx)
	if err != nil {
		log.Fatal(err)
	}
	var shown *pdiggerfactory.Tray
	for i := range trays {
		tray := &trays[i]
		arrived, leaving, ok := tray.Window()
		if !ok {
			continue // a stop that never settled
		}
		fmt.Printf("tray %d: arrived %s, leaving %s\n", tray.N, pdiggerfactory.FormatClock(arrived),
			pdiggerfactory.FormatClock(leaving))
		for _, stone := range tray.Stones {
			if stone.Status == pdiggerfactory.TrayStoneRemoved && *stone.Certain {
				fmt.Println("  removed:", stone.ID)
			}
		}
		if tray.N == show && shown == nil {
			shown = tray
		}
	}
```

`Window()` is the media span to replay or to show. To show a point on the unit, seek the shared session there; a seek
changes what every viewer sees, so the example sends dry runs ([examples/traypoints](../examples/traypoints/main.go)):

```go
	for _, point := range []*pdiggerfactory.TrayPoint{shown.Arrived, shown.Leaving} {
		would, err := client.Session().Seek(ctx, point.T, pdiggerfactory.InVideo(shown.Video), pdiggerfactory.DryRun())
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("seek to %s would show %s from %s\n", pdiggerfactory.FormatClock(point.T), would.Video,
			pdiggerfactory.FormatClock(would.AnchorMedia))
	}
```

Over the fixture `dry1-0018-0153` it prints `tray 1: arrived 0:22.5, leaving 2:24.0`, the six stones removed for
certain (`  removed: 20261122T023847.0Z-1-105` and the others), then `seek to 0:22.5 would show dry1 from 0:22.5` and
`seek to 2:24.0 would show dry1 from 2:24.0`.

## Pacing

By default a replay goes as fast as the file can be read. `Pace: PaceRealtime` delivers each update at its frame's
media time, divided by `Speed`, as if a unit were playing the recording now: a way to drive a dashboard or a viewer
from a recording.

```go
replay, err := pdiggerfactory.OpenReplay(ctx, os.Args[1], &pdiggerfactory.ReplayOptions{
	Pace: pdiggerfactory.PaceRealtime, Speed: speed})
if err != nil {
	log.Fatal(err)
}
defer replay.Close()
if h := replay.Header; h != nil {
	fmt.Printf("%s: %d frames, build %s\n", h.Name, h.Frames, h.Build)
}
var last pdiggerfactory.Hands
for update, err := range replay.Updates(ctx) {
	if err != nil {
		log.Fatal(err)
	}
	hands := update.State.Hands
	if !update.Final && (hands.Left.State != last.Left.State || hands.Right.State != last.Right.State) {
		fmt.Printf("%s left hand %s, right hand %s\n", pdiggerfactory.FormatClock(update.State.T),
			hands.Left.State, hands.Right.State)
		last = hands
	}
}
```

(from [examples/pacing](../examples/pacing/main.go): on the fixture recording aug28-1270-1300 it prints
`21:15.4 left hand free, right hand touch` 5.4 s of media after the first frame, then
`21:17.8 left hand touch, right hand hinge` when the right hand picks up stone 2660 and tips it up on its edge)

`ReplayOptions` also take an `HTTPClient` for URLs and a `Clock` for the pacing (a `factorytest.Clock` makes a paced
replay instant in tests). Pacing starts again at a session change and whenever the media time goes back.

## Comparing with the published summaries

The JSON form of a summary (`json.Marshal(result.Summary)`) has the keys and the rounding of the summaries in
`recordings.json` and in the golden files, so a replay of a published recording can be checked against its published
summary after parsing both. The SDK's opt-in recording tests do exactly that ([testing.md](testing.md)).
