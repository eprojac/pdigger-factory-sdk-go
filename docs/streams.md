# Streams

The mid level delivers frames as the unit releases them. `client.Streams()` has three streams, each an endless
`iter.Seq2[StreamItem, error]`:

| Stream | Transport | Items |
|---|---|---|
| `Frames(ctx, opts...)` | server-sent events (`GET /api/v2/session/frames/stream`) | frames with the layers you ask for, and the side events |
| `Poll(ctx, opts...)` | long-poll (`GET /api/v2/session/frames`), for environments without streaming | the same frames; a state change is a `ClockItem`, a worker change a `WorkerItem` |
| `RawPackets(ctx, opts...)` | NDJSON or SSE (`GET /api/v2/raw/stream`) | the model's raw packets (`sip1`), and gaps |

The interpreter is built on `Frames`. Use the streams directly when you need layers the interpreter does not read
(overlay, scene, raw), or the frames themselves.

## Items

Use a type switch on the item:

| Item | Fields | When |
|---|---|---|
| `*FrameItem` | `Frame`, `ID` (`<boot>-<cursor>`), `Cursor`, `IsRevision`, `IsBackfill` | a frame |
| `*ClockItem` | `State` (the shared session), `ServerNow` (the unit's clock, epoch seconds) | the shared state changed; with `ClockOnConnect(true)` (the interpreter's default) also after every connect |
| `*WorkerItem` | `Worker` (the interpreter worker's status) | about every 5 s |
| `*GapItem` | `Count` | the unit skipped that many items for this consumer (it fell more than 16 behind) |
| `*ResetItem` | `Boot`, `Cursor` | the unit restarted; the stream goes on from the newest frame |
| `*PacketItem` | `Packet` (`*Sip1Packet`), `ID`, `Cursor` | a raw packet (`RawPackets` only) |

```go
frames := 0
stream := client.Streams().Frames(ctx, pdiggerfactory.Layers(pdiggerfactory.LayerWorld), pdiggerfactory.Observe(true))
for item, err := range stream {
	if err != nil {
		log.Print(err)
		continue
	}
	switch it := item.(type) {
	case *pdiggerfactory.FrameItem:
		frames++
		stones := 0
		if it.Frame.World != nil { // nil when the world came late (Backfill(true) waits for it)
			stones = len(it.Frame.World.Stones)
		}
		fmt.Printf("frame g %d at %s: %d stones\n", it.Frame.G, pdiggerfactory.FormatClock(it.Frame.T), stones)
	case *pdiggerfactory.ClockItem:
		fmt.Printf("clock: session %d plays %s, playing %v\n", it.State.Session, it.State.Video, it.State.Playing)
	case *pdiggerfactory.WorkerItem:
		fmt.Printf("worker: %s\n", it.Worker.State)
	case *pdiggerfactory.GapItem, *pdiggerfactory.ResetItem:
		fmt.Printf("%s: frames were skipped\n", item.Kind())
	}
	if frames == 3 {
		break // closes the connection
	}
}
```

(from [examples/streams](../examples/streams/main.go); against the fixture recording joined-mid-move it prints
`clock: session 1795314878 plays dry1, playing true`, then `frame g 399 at 1:19.6: ...` and the next two frames)

## What a stream does for you

- **Resume.** When the server ends a stream or the connection drops, the SDK reconnects with `Last-Event-ID` set to
  the last event id, so the loop sees one endless stream. Inside the unit's retained window (512 frames) nothing is
  lost. Failed connections are retried after the server's `retry_ms`, else its `Retry-After`, else the client's
  backoff (250 ms doubling to 5 s, with jitter); `MaxReconnects(n)` gives up after n failures in a row, with a
  `stream_closed` error.
  `Resume(false)` turns this off: the first failure is yielded as it is and ends the stream. A reply that trying again
  cannot fix (a 400 or 404) is yielded as an error and ends the stream. An event that does not decode is yielded as a
  `decode` error (`Op` "decode") and the stream goes on, its id still counting for the resume: its data must be a JSON
  object with the fields of its type (SDK-DESIGN.md 2.3 rule 9: a frame's `cursor`, `session`, `g` and `i` whole
  numbers, `t` and `dt_past` numbers, `video` a string, `past_valid` a boolean, and its layers' fields; a member that
  is null counts as missing), read further inside by the models. A line or an event over 64 MiB ends the stream with
  a `decode` error (the unit would send the same event again).
- **Limits.** `ConnectionLimit(n)` and `ConnectionMaxSeconds(s)` ask the server to end each connection after n
  frames or s seconds. The SDK counts both itself (the seconds on the client's clock, from the reply's headers),
  closes the connection when one is reached and reconnects at once: the 20.20 unit sends nothing more after a limit
  but closes the connection only about 10 s later, and waiting for that would cost 10 s of frames.
- **The clock on connect.** The unit sends a `clock` event when the shared state changes, not when a stream connects.
  With `ClockOnConnect(true)` (the interpreter's default) the SDK reads the state (`GET /api/v2/session`, one try with
  a 1 s timeout on the client's clock) after every connect and yields it as a `ClockItem` before the frames, so wall
  times are known from the first frame and after every reconnect; a failed read is skipped.
- **Viewers.** A stream, and the interpreter's stream, count as a viewer unless opened with `Observe(true)` or on a
  client made with `WithObserve(true)`: see [concepts](concepts.md#the-shared-session).
- **Late layers.** A frame released without a layer (`Missing: ["world"]`; on the 20.20 unit, on build
  `claude-20261114-int17` 3 to 4 % of the frames of dry1 and dry2 and none of aug28 and jul30, on build
  `claude-20261122-hr4x1` 5.6 % of those of dry1) is never sent again. With `Backfill(true)` (the interpreter's default)
  the stream holds such a frame back, with every frame after it, reads the missing layers with `GetFrame` 1.5 s after it
  arrived (up to 3 tries, 1 s apart, each with a 1 s timeout, all on the client's clock: `WithClock`), merges them in
  and releases the held frames in order; the completed frame has `IsBackfill`. A frame that cannot be completed (no
  longer retained, its session gone, the tries used up) is released with the layers that did come merged in
  (`IsBackfill` when one did) and the others still in `Missing`. Side events are never held. The cost is up to about
  4.5 s of latency while a world is late (on 20.20, every late world was there at the first try, 1.5 s after release,
  on build `claude-20261114-int17`, and every late world of dry1 was read back on build `claude-20261122-hr4x1`).
- **Revisions and duplicates.** A frame whose `World.Rev` is higher than that of a frame already delivered for the
  same session and `g` comes again with `IsRevision`; a frame repeated with the same or a lower revision is dropped.
- **Reset.** A `Last-Event-ID` of another boot (the unit restarted) gets a `ResetItem`, and the stream goes on from
  the newest frame.

## Options

| Option | Default | What |
|---|---|---|
| `Layers(...)` | overlay, scene, cards, world (the interpreter: world, cards) | the layers of each frame (`LayerRaw`, `LayerOverlay`, `LayerScene`, `LayerCards`, `LayerWorld`) |
| `Observe(bool)` | the client's (`WithObserve`), false | true: the stream does not count as a viewer |
| `Backfill(bool)` | false (the interpreter: true) | complete late layers in order |
| `Resume(bool)` | true | reconnect with `Last-Event-ID` |
| `After(cursor)` | the newest frame | start after this cursor |
| `LastEventID(id)` | none | resume after an event id of an earlier stream |
| `Precision(p)` | `rounded` | `full` for unrounded numbers |
| `MaxReconnects(n)` | 0 (never give up) | end with `stream_closed` after n failed reconnects in a row |
| `OnReconnect(fn)` | none | called before each reconnect after a failure |
| `ConnectionLimit(n)`, `ConnectionMaxSeconds(s)` | none | end each connection after n frames or s seconds (counted by the SDK) |
| `ClockOnConnect(bool)` | false (the interpreter: true) | `Frames` only: read the shared state after every connect and yield it as a `ClockItem` |
| `RawFormat(f)` | `FormatNDJSON` | `RawPackets` only: NDJSON or SSE |
| `PollLimit(n)`, `PollWait(ms)` | the server's (8 frames, 1000 ms) | `Poll` only |

## Raw packets

`RawPackets` follows the model's own output, one packet per frame before the world: boxes, joints, stone tuples, the
packed maps and the time each stage of the model took. The [codec](low-level.md#the-codec) decodes the packed parts.

```go
packets := 0
for item, err := range client.Streams().RawPackets(ctx, pdiggerfactory.Observe(true)) {
	if err != nil {
		log.Fatal(err)
	}
	p, ok := item.(*pdiggerfactory.PacketItem)
	if !ok {
		continue // a gap: packets were skipped
	}
	stones := codec.StoneItems(p.Packet.Stones.Items) // tuples [u, v, w, h, score, X, Y, Z, w_m, h_m, yaw]
	fmt.Printf("g %d: %d stones detected, the model took %s ms (encoder %s ms)\n", p.Packet.G, len(stones),
		pdiggerfactory.FormatFixed(p.Packet.MS["total"], 1), pdiggerfactory.FormatFixed(p.Packet.MS["encoder"], 1))
	if packets++; packets == 5 {
		break // closes the connection
	}
}
```

(from [examples/rawpackets](../examples/rawpackets/main.go); on the fixture recording dry1-0296-0306-full the first
line is `g 1480: 40 stones detected, the model took 241.0 ms (encoder 163.3 ms)`)

## Channels and cancellation

Breaking a loop, or cancelling its context, closes the connection and stops the goroutine that reads it; a stream
holds no resources after its loop ends. For `select`-based code, `pdiggerfactory.Chan(ctx, stream)` runs a stream in
a goroutine and delivers `ChanResult{Value, Err}` values on a channel, closed when the stream ends or `ctx` is
cancelled:

```go
// The same frames over the long-poll, as a channel: stop after three frames or five seconds.
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()
polled := pdiggerfactory.Chan(ctx, client.Streams().Poll(ctx, pdiggerfactory.Observe(true)))
```

Cancel the context to stop a `Chan` early: that also closes its stream.

## One connection, no resume

`client.API().StreamFrames` and `client.API().RawStream` open one connection and yield its raw server-sent events
(`RawEvent`: `Event`, `ID`, `Data`, `Retry`; `Decode` parses the data, without the checks of rule 9). They end at the `Limit` or `MaxS` asked for,
counting them themselves, instead of waiting the 10 s the unit takes to close. They do not resume, backfill or drop
duplicates; the streams above do. `client.API().PollFrames` is one long-poll call.

To resume such a stream yourself, send the `ID` of the last event you handled as `LastEventID`. The parsers are
exported for bodies you read yourself: `NewSSEParser(r)` follows the SSE specification (an `id:` line takes effect
when its event is dispatched, so after a stream that broke inside an event `LastEventID()` still names the last event
delivered), and `NewNDJSONParser(r)` reads one JSON value per line. Both return a `decode` error for a line, or an
event, over 64 MiB.
