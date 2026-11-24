# The interpreter

The interpreter turns frames into what happened: typed events, activities (moves and push bursts), the hands' states,
the stone inventory, per-stone tracks, trays with stone ids of the SDK's own that are never reused, a summary, and a
sentence for each of them. The rules and every sentence are the same in the four languages, and the golden tests check
them byte for byte against the output of the maintainers' reference interpreter (not published), the golden files in
fixtures/golden ([spec/SDK-DESIGN.md](../spec/SDK-DESIGN.md), section 4).

## Following a unit

`client.Interpreter(opts...)` follows the shared session. Its stream asks for the `world` and `cards` layers, backfills
late worlds (so frames come complete and in order) and counts as a viewer, which keeps the unit's worker interpreting.
It takes the options of [Streams.Frames](streams.md), for example `Observe(true)` for a tool that only looks, or
`OnReconnect` to log reconnects:

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	interpreter := client.Interpreter(pdiggerfactory.OnReconnect(func(attempt int, err error) {
		log.Printf("reconnecting (attempt %d): %v", attempt, err) // the stream resumes where it stopped
	}))
	for activity, err := range interpreter.Activities(ctx) {
		if err != nil {
			log.Print(err)
			continue
		}
		fmt.Println(activity.Describe()) // 0:20.5 Both hands push stones 86, 90 and 94 (6 pushes in 0.6 s)
	}
}
```

(from [examples/activities](../examples/activities/main.go))

| Method | What |
|---|---|
| `Updates(ctx)` | an `*Update` per frame: its new events, the activities it completed, the tray facts it made, the state after it |
| `Events(ctx)` | just the events, in world order |
| `Activities(ctx)` | just the activities, as frames complete them |
| `TrayFacts(ctx)` | just the tray facts: trays arriving and leaving, tray stones removed, set down or no longer sure ([trays](#trays-and-tray-stones)) |
| `WaitFor(ctx, match, timeout)` | the first event that matches (`OfType(...)`, `AboutStone(id)`, any `func(Event) bool`, or nil for any) |
| `State()` | the `*InterpreterState` after the newest frame |
| `Summary()` | the `*Summary` so far |
| `Track(id)` | the path of a stone's solid in this session, from the frames fed |
| `Trays()` | the trays so far, oldest first: the newest 256 that have left and the open one |
| `Finish()` | closes what is still open (moves as `interrupted`, push bursts ended) and returns those activities; the open tray leaves |
| `End()` | `Finish` as the final `*Update`: the activities still open and the facts of the open tray leaving |

Connection drops, streams the server ends and server restarts are handled inside the stream. The iterators yield an
error for an event that does not decode (`Op` "decode", SDK-DESIGN.md 2.3 rule 9) and go on, and `WaitFor` waits on
past it; they yield an error the stream cannot recover from (a reply such as 400 or 404, or a used-up
`MaxReconnects` budget) and end. They also end when the loop breaks or the context ends
(then they yield the context's error).

A later loop over the same interpreter goes on where the last one stopped: first with the rest of the frame that loop
stopped in (the events, activities and tray facts it did not yield; after `WaitFor`, the events after the match and
all of that frame's activities and tray facts), then with the frames after it (the stream resumes with `Last-Event-ID`, while the unit still
holds those frames). So `WaitFor` followed by `Updates`, `Events` or `Activities` misses nothing in between; `Finish`
leaves the activities a loop did not yield to the next loop. Consume an interpreter from one loop at a time; `State`,
`Summary`, `Track` and `Trays` may be called from any goroutine. A stream the server ends with `Resume(false)` ends
with the final update, as a replay does; a live loop otherwise has no end of its own: when a program stops following,
`End()` (or `Finish()`) closes what is open. A loop that breaks, fails or is cancelled gets no final update.

An `Update` has `Frame`, `Events`, `Activities`, `TrayFacts`, `State`, and three flags: `IsRevision` (the frame revised
the world of a frame already seen, see [concepts](concepts.md#layers)), `IsBackfill` (its world came late and was read
back), and `Final`, for the last update of a replay, of a stream the server ended and of `End()` (after the last
frame: `Frame` is nil, `Events` is empty, `Activities` are the ones still open, closed, and `TrayFacts` those of the
open tray leaving).

## Events

An `Event` is what the world says happened to a stone in a frame: `Type` (the wire name), `G`, `T`, `Wall`,
`Session`, `Video`, `StoneID` (nil when the event names none), `Hand` (`left`, `right`, `both` or empty), `Why`,
`Into` (a split's ids), `Source` (`image` for a stone the model never boxed), `Stone` (the stone's snapshot in that
frame's world, nil when it is not there) and `TrayStone` (the id of the [tray stone](#trays-and-tray-stones) the world
id stood for on the open tray before this frame, also for an event of a revision, empty when none: `StoneID` is the
world's id, which the engine's tracker renames and reuses).

`event.Describe()` is the media time as a clock and a sentence: `21:17.8 Right hand lifts stone 2660`. The clock is
`M:SS.d` below an hour and `H:MM:SS.d` from an hour on. The hand is `Left hand`, `Right hand`, `Both hands` (with the
verb in the plural: `Both hands push stone 94`) or `A hand` when the event has no side.

| Type | Constant | Sentence |
|---|---|---|
| new | `EventNew` | `stone 72 appears`; `stone 72 appears (found in the image)` for a stone the model never boxed |
| confirm | `EventConfirm` | `stone 72 is confirmed` |
| freeze | `EventFreeze` | `stone 72 gets its final shape` |
| push | `EventPush` | `Right hand pushes stone 72`; `stone 72 is pushed along its row` (why `course`); `stone 72 is reseated` (why `reseat`); `stone 72 is pushed` without a side |
| lift | `EventLift` | `Right hand lifts stone 72` |
| hinge | `EventHinge` | `Right hand tips stone 72 up on its edge` |
| carry | `EventCarry` | `Right hand carries stone 72` |
| place | `EventPlace` | `Right hand places stone 72` |
| drop | `EventDrop` | `Right hand drops stone 72` |
| gone | `EventGone` | `Right hand takes stone 72 out of view` |
| delete | `EventDelete` | `stone 72 is removed (no longer seen)`; the reasons: no longer seen, overlapped another stone, outside the board, merged with a neighbour, its place is empty, a hand emptied its place, carried away |
| split | `EventSplit` | `stone 72 splits into 72 and 80`; `stone 72 is trimmed` (one part); `stone 72 splits` |
| a newer type | `EventOther` in counts | `stone 72: <type>` |

A missing stone id prints `?`.

## Activities

An `Activity` is what the SDK joins from events at the scale people talk about. `Kind` is `KindMove` or `KindPush`;
both kinds have `Hand`, `Start` and `End` (each a `Moment` of `G`, `T` and `Wall`), `Duration` (seconds of media),
`Session` and `Video`.

**A move** is one stone taken and put somewhere, per stone within a session:

- It opens on a `lift` or a `hinge` of a stone with no open move. `From` is the stone's position in that frame.
- While it is open, a `carry` sets `Carried` and a `hinge` sets `Tipped`. If an event of the move has another side
  than the move's hand, the hand becomes `both`.
- It closes on a `place` (`Outcome` `placed`), a `drop` (`dropped`), a `gone` (`carried_away`) or a `delete` of the
  stone (`removed`). `To` is the stone's position for `placed` and `dropped`, and `Distance` the x-y distance from
  `From` to `To`, rounded to 3 decimals, in units of the template gauge.
- A `place`, `drop` or `gone` with no open move makes a move with `Partial` set: the stream or recording joined in the
  middle of it.
- At a session change, and at `Finish`, open moves close as `interrupted`.

**A push burst** is a hand pushing stones about: the pushes of one side (`push` events with a side and no why) at
most 1 s apart. `Stones` are the pushed stones' ids, ascending, and `Pushes` the number of pushes. A burst closes when
a frame with a world comes more than 1 s after its last push, at a session change, and at `Finish`.

`activity.Describe()` starts with the clock of the start, and its verbs agree with the hand as for events. The
examples are from the golden files of the fixture recordings:

| Activity | Sentence | Example |
|---|---|---|
| placed, carried | `{hand} moves stone {id} {distance} units in {duration} s` | `21:17.8 Right hand moves stone 2660 0.84 units in 4.0 s` |
| placed, tipped | `{hand} tips stone {id} and sets it down ({duration} s)` | `0:36.3 Right hand tips stone 117 and sets it down (0.2 s)` |
| placed | `{hand} lifts stone {id} and sets it down ({duration} s)` | |
| dropped | `{hand} drops stone {id} after {duration} s` | `0:50.5 Left hand drops stone 128 after 0.2 s` |
| carried away | `{hand} takes stone {id} away ({duration} s)` | `1:19.2 Left hand takes stone 155 away (1.0 s)` |
| removed | `{hand} lifts stone {id}; it is removed after {duration} s` | |
| interrupted | `{hand} is still holding stone {id} ({duration} s so far)` | `1:19.2 Left hand is still holding stone 155 (0.6 s so far)` |
| partial | the same at the clock of the end, plus ` (joined mid-move)` | `1:20.2 Left hand takes stone 155 away (0.0 s) (joined mid-move)` |
| push burst | `{hand} pushes {stones} ({n} pushes in {duration} s)` | `0:20.5 Both hands push stones 86, 90 and 94 (6 pushes in 0.6 s)`, `0:21.9 Left hand pushes stone 86 (1 push)`, `1:06.4 Right hand pushes stones 89, 94, 102, 103, 107, 108 and 1 more (16 pushes in 2.6 s)` |

A carried move without a distance reads `{hand} moves stone {id} in {duration} s`. Durations have one decimal and
distances two.

**Order.** Within one frame the events come in world order, then the activities that frame closed, sorted by end
frame, start frame, kind (moves first), stone id, hand, then the tray facts it made. Activities closed before a frame's
events (a stale burst, a session change) come before them, and so do the facts of a tray that leaves at a session
change.

## Trays and tray stones

The world's stone ids are the engine tracker's. It renames a stone it lost and found again, and it can keep an id on
another stone: a stone set down where a removed one lay, a neighbour pushed into its place, a row of equal stones pushed
one place along while the ids stay on the places. For anything that must hold over time ("stone X was removed"), use the
**tray stones**: the interpreter follows each tray from the moment it has arrived and settled to the moment it is about
to move out, and gives every stone on it an id that is never reused, no other tray stone is ever given it:
`{first seen}-{tray}-{world id}`, for example `20261122T023847.0Z-1-117` (the server's wall time in UTC when the stone
was first seen on tray 1, under world id 117; without a clock `s{session}t{media time}-{tray}-{world id}`; a second
stone first seen on the tray in the same tenth of a second under the same world id takes `-2` at the end, a third
`-3`).

A tray stone says whether its identity is still **sure**, and a fact about it is **certain** or **unclear**. Tray facts
are final: a fact, once given, is never revised or withdrawn, and a stone that is not sure (fact `TrayUnsure`) is sure
again only through the unit's speckle check as its tray leaves, once, with a `TrayIdentified` fact
([below](#the-units-speckle-check)). On a worked tray the world stone a tray stone stands for can turn out to be another
stone; the rules mark a stone not sure whenever what the world shows leaves another reading, but they do not tell which
of the not-sure links are wrong. The rules prefer saying "unclear" to guessing:

- A **tray** is a stop of at least 2 s. It has **arrived** at the first calm frame with every stone confirmed and no new
  stone for five frames (a calm frame: both hands free for three frames in a row, no hand event, no stone new, deleted
  or split). It is **leaving** just before it moves out: at its last calm frame, when no hand worked after it (else at
  its last frame).
- A stone stays **sure** while nothing could have swapped it: it stops being sure when it was pushed, when a stone of
  its size next to it was pushed, when its box overlaps another stone, when it is not seen at two calm moments, or when
  a stone of its size appeared next to it while a hand held it. A stone without a look chain that is not seen at two
  calm moments while nothing could have changed it since it was last seen is first held in question (not sure,
  `Question` set, no fact yet): it is sure again once the world sees it again on its place, with no hand at work, no
  held stone, no stone a hand took away, no push on the tray and no frame the SDK could not read (a frame without a
  world, a jump in `G`, a `gap` or `reset` of the stream, a revision, the moving frames of a tray that goes on) from its
  last sight until then, no hand on it or near it, no other stone of its size within 0.12 of its place, and no look of
  its world stone that matches another stone in question or lost; otherwise it stops being sure. When the question is
  settled on a calm moment that does not show the stone (at once at its doubt, or by a sight before that calm moment),
  the next calm moment that shows it, or the tray's last frame when the tray leaves first and that frame shows it, must
  find nothing at work since either. A stone with a look chain is never held in question: its doubt is a question only
  its looks settle. A stone absent from one or two quiet frames keeps its identity if the same world ID returns at its place with
  a compatible size. Those missing frames cannot become the leaving point. When the world renames a stone, the tray
  stone follows only if exactly one new world stone fits,
  close by and within 3 s.
- A **removal** is certain when the stone's place is empty at a calm moment, as far as the world shows, and no world
  stone could be it. An **addition** is certain when a new stone stays at one place over two calm moments while no stone
  of its size is lost or not sure. When the tray leaves, a lost stone is matched to a new world stone near it only
  with evidence that they are the same stone (the unit's speckle check pairs them, their looks agree, or no hand worked
  near either since the stone was last seen); everything else is reported as unclear, so a stone's id never passes to
  a stone that may be another.

Some of the facts of `dry1-0018-0153`, a busy tray of dry1, from 0:38.7 to 0:59.4 (`interpreter.TrayFacts(ctx)`,
`fact.Describe()`):

```text
0:38.7 tray 1: stone 20261122T023847.0Z-1-105 is not sure any more (a stone next to it was pushed)
0:38.7 tray 1: stone 20261122T023847.0Z-1-126 is not sure any more (a stone next to it was pushed)
0:45.5 tray 1: stone 20261122T023847.0Z-1-97 is not sure any more (it was pushed)
0:45.5 tray 1: stone 20261122T023847.0Z-1-117 was removed
0:45.5 tray 1: stone 20261122T023847.0Z-1-127 was removed
0:59.4 tray 1: stone 20261122T023847.0Z-1-126 was removed
```

and its end, where the speckle check has settled all but one of the stones the worker pushed, and the SDK reports
the unresolved additions:

```text
2:24.0 tray 1: stone 20261122T024126.9Z-1-284 not recognised (unclear)
2:24.0 tray 1 leaving: 44 stones (30 sure), 0 added, 6 removed, 18 unclear
```

| Fact (`Kind`) | Sentence |
|---|---|
| `TrayArrived` | `{clock} tray {n} arrived: {stones} stones` |
| `TrayUnsure` | `{clock} tray {n}: stone {id} is not sure any more ({why})` |
| `TrayRemoved`, certain | `{clock} tray {n}: stone {id} was removed`, with ` (carried away)` when a hand took it out of view |
| `TrayRemoved`, unclear | `{clock} tray {n}: stone {id} not found (unclear)` |
| `TrayAdded`, certain | `{clock} tray {n}: stone {id} was set down` |
| `TrayAdded`, unclear | `{clock} tray {n}: stone {id} not recognised (unclear)` |
| `TrayIdentified` | `{clock} tray {n}: stone {id} confirmed by its speckle on world stone {world id} ({score})`; `recognised` or `found again` in place of `confirmed` when the stone was on another world stone or lost; `recognised by its look` (or `found again by its look`) for a look's link |
| `TrayLeaving` | `{clock} tray {n} leaving: {stones} stones ({sure} sure), {added} added, {removed} removed, {unclear} unclear` |

A `TrayFact` has `Kind`, `Tray`, `Session`, `Video`, `Stone` (the tray stone's id, empty for arrived and leaving),
`WorldID` (the world id it stands for now, nil when none), `Certain` (for removed and added), `Why`, `G`, `T`, `Wall`,
and the counts of arrived and leaving (`Stones`, `Sure`, `Added`, `Removed`, `Unclear`). `interpreter.Trays()` lists the
trays so far (those that have left, the newest `TraysKept` = 256, and the open one) and `replay.Trays(ctx)` every tray
of a recording. A `Tray` has `Session`, `Video`, `N`, the points `Arrived` and `Leaving` (`*TrayPoint`, a `Moment` of
`G`, `T` and `Wall`; nil until it has arrived, and while it is on), `Stones` (`TrayStone`: `ID`, `WorldID`, `Status`
`TrayStoneOn`, `TrayStoneLost` or `TrayStoneRemoved`, `Sure`, `Added`, `Certain`, `Born`, `Place`, `Size`), its
`Facts`, `Window()` (the media span from arrival to leaving; ok is false until it has left) and `Stone(id)`. The two
points are the ones to jump between: on the shared session, `client.Session().Seek(ctx, point.T,
pdiggerfactory.InVideo(tray.Video))` shows one ([replay](replay.md#trays)). The state has the open tray (`Tray`).

A tray's report is the SDK's simple mode (SDK-DESIGN 4.7): `tray.Report()` once the tray has left (nil before),
`interpreter.TrayReports(ctx)` from a unit as each tray leaves, and `replay.TrayReports(ctx)` for every tray of a
recording. A `TrayReport` has the tray's `Session`, `Video` and number (`Tray`), its points `Arrived` and `Left`, the
number of `Stones` it arrived with, its changes `Removed`, `Added` and `Moved` (`TrayChange`: `Kind`, `Stone`, `At`,
`Certain`, `Code`, `Place`, `Size`, `Distance`, `Confidence`), the counts `Unclear` and `Unsure` (the stones still on
that are not sure), and `Complete`: no unclear change and no stone that is not sure. `Describe()` writes it as text, and
`json.Marshal` writes it with the other languages' names, for data. A stone is reported moved only when the SDK is sure it is
the same stone, saw it at the leaving point and something touched it on the tray, and it moved more than `Moved` (0.75)
times its short side once the tray's own shift is taken out.

`tray.Open` is the open account of the tray's leaving (`TrayOpen`, SDK-DESIGN 4.6.8: for each stone left open, the
readings the evidence still allows; nil until the tray has left), and `TrayFact.Code` gives each fact's reason as a code
(`carried_away`, `pushed`, `not_recognised`, ...). A tray stone also has `FirstPlace`, `LeftPlace` and `Touched`, which
the report reads, and `Question` while it is held in question; with the stones' looks, a fact may be `TrayIdentified`
and a tray stone carries `Looks`, `ByLook`, `LookLost` and `Open`; with the unit's speckle check, `Speckle`, `BySpeckle`
and `Evidence` (below).

### Confidence

Every change of a report has a `Confidence`, the probability that it is right: the share of changes of its kind that
were right on 150 trays of the four complete recordings checked against their video (the maintainers' offline checks,
not published: each stone followed by its look both ways, and a camera check of the certain removals), as
`(right + 1) / (checked + 2)` (SDK-DESIGN 4.6.13). Its kind is the change, whether it is certain, and what the unit's
speckle check found of the stone at the other moment (`TrayStone.Evidence`): for a removal, whether the stone is on the
tray as it left; for an addition, whether it was on the tray as it arrived.

| Change | Certain | not found | undecided | found |
|---|---|---|---|---|
| removed | yes | 0.98 | 0.50 | 0.25 |
| removed | no | 0.71 | 0.58 | 0.09 |
| added | yes or no | 0.96 | 0.52 | 0.02 |

A move is `MovedConfidence`, 0.87. The numbers are constants (`ConfidenceRemovedCertainNotFound` to
`ConfidenceAddedUnclearFound`), the same in every language, so a client keeps the changes at or above a threshold of its
own: at 0.90, dry1's tray 2 (the fixture's tray 1) keeps its six certain removals and nine of its thirteen additions,
all fifteen right by the camera check.

### The unit's speckle check

Every report uses it; nothing needs to be called. Since build `claude-20261123-speckle3` the unit compares each tray's
stones between its arrival and a later moment by their speckle (the grain of each stone's face), on its GPU, and sends
each answer once, in the world (`world.speckle`; [how the unit does it](BEHAVIOUR-20.20.md#8-the-speckle-answers)); the
four published recordings carry answers made afterwards. The answer for the tray's last calm frame is the leaving's
(SDK-DESIGN 4.6.13): as the tray leaves, the stones it pairs are sure, confirmed on their world stone or linked again
where the world had lost them or put them on other world stones, and each change takes its confidence from what it
found. A tray whose hands worked after its last calm frame has no leaving answer: its report is made without one, its
changes `undecided`.

Reading the answers yourself is advanced mode: to look into an unclear change, to check a tray by hand, or for rules of
your own. The interpreter reads each answer as a tray's (`TraySpeckle`): `Update.Speckle` holds the answers a frame
gave, `Interpreter.Speckle()` the answers so far (the newest 1024) and `Replay.Run`'s `Result.Speckle` every answer of a
recording, and while the tray is on, each of its stones in the answer takes its part as `Speckle` (`StoneSpeckle`). As
the tray leaves, `TrayStone.BySpeckle` says the leaving's answer paired the stone, and `TrayStone.Evidence` what it
found of it at the other moment (`EvidenceFound`, `EvidenceNotFound` or `EvidenceUndecided`: a look-alike, or a stone it
did not check). An answer at another moment, or a score below the clean bar, is for you to weigh: the tray rules use
only the leaving's clean finds. The last answer for the first tray of the fixture recording `dry1-0018-0153` (tray 2 of
the published dry1), as `DescribeSpeckleTxt` writes it, begins:

```text
732 2:26.0 tray 1: speckle of 0:22.5 and 2:24.2: 42 stones, 30 clean, 26 paired, 24 the same
  20261122T023847.0Z-1-84 arrival 0.632 0.112 clean found 20261122T023847.0Z-1-84 pair 20261122T023847.0Z-1-84
```

and, further down:

```text
  20261122T023847.0Z-1-104 arrival 0.481 0.090 clean found 20261122T023847.0Z-1-104 pair 20261122T023847.0Z-1-104
  20261122T023847.0Z-1-105 arrival 0.102 0.079 found world 193
  20261122T023847.0Z-1-106 arrival 0.706 0.102 clean found world 266
  20261122T023847.0Z-1-107 arrival 0.720 0.115 clean found 20261122T023847.0Z-1-106 pair 20261122T023847.0Z-1-106
```

Stones 84, in question since 2:18.8, and 104, pushed at 2:09.6, are the same stones at the leaving, so they stay sure;
105, removed at 1:05.4, is found nowhere clean; 107, which the world had lost, is found and paired on world stone 191,
where the SDK had linked tray stone 106, and 106 on world stone 266, which no tray stone stood for. As the tray leaves,
the SDK links them so: 107 found again on 191, 106 recognised on 266 (a camera check of this tray: 39 of its stones
right, none wrong). A `StoneSpeckle` has `Stone`, `Side` (`SpeckleArrival`: the stone as it was at the arrival, checked
at the later moment; `SpeckleLater`: the other way), `Score` and `Second` (nil: not checked), `Clean` (a score of at
least 0.300 and at least twice the second), `Found` (the tray stone at the other moment that its best place lies on, nil
when none; `FoundWorld` the world stone), `Pair` (the tray stone the answer pairs it with, or nil) and `Same()` (paired
with itself). A `TraySpeckle` has `Tray`, `Session`, `Video`, `G`, `T` and `Wall` (the frame it came in), `UnitTray`
(the answer's own tray number, the unit's count), `Ref` and `At` (the two moments: `G` and `T`), `Device`, `GPUMS`,
`WallMS`, `Stones`, `Stone(id)` and `Describe()`.

What the SDK can not see, since it sees only the world: a box the model sees on an empty slot when the tray arrives
becomes a tray stone, and a stone later set into that slot keeps its id; and a stone the model stops seeing for good
while it stays on the tray looks removed: unclear when the tray leaves, or for certain when a hand touched it before it
was lost and no world stone could be it (3 of the 52 certain removals in a camera check of the four complete recordings
were such stones, each one a stone a hand had moved). On busy trays, where hands push rows of equal stones, the
speckle check as the tray leaves settles most pushed stones; where hands worked after the tray's last calm frame there
is no leaving answer, and most pushed stones end up not sure. Stones set down are mostly unclear: their confidence
says how far to rely on each.

## State and tracks

`InterpreterState` is a snapshot after a frame: `Session`, `Video`, `G`, `T`, `Wall`, `Stones` (the newest world's
stones by id; `StoneIDs()` sorted, `Stone(id)`), `Hands` (`Left` and `Right`, each with `State` free, touch, grasp,
hinge or carry, the `Stone` it holds and its `Card` title, such as "Holding a stone"), `Board`, `Stats`, `OpenMoves`
(moves begun and not yet ended, by stone id), `Tray` (the open [tray](#trays-and-tray-stones) with its tray stones, nil
between trays) and `Counters`. Later frames do not change a snapshot.

`Track(id)` is the path of a stone's solid in this session, oldest first: a `TrackPoint` (`G`, `T`, `Pos`) for every
frame where the stone moved, at most the newest 512, for the 4,096 most recently moved stones. The server's own track
of a stone since the session began, with its 2-D path, is `client.API().GetStone(ctx, id)`.

## The summary

`Summary()` (and a replay's `Summary`, `Run`) counts what was seen: frames, sessions, gaps, events by type, moves by
outcome with their mean duration and distance, push bursts by hand, stones, trays (opened and arrived, tray stones at
the arrivals, certain and unclear additions and removals, tray stones that stopped being sure), and the seconds each
hand spent in each state (0.2 s per frame with a world). `Describe()` is its text, the same in every language:

```text
frames: 151 (with world 151, without 0, revisions 0)
media: 0:30.0, sessions 1, gaps 0
stones: last 33, max 49, confirmed 33
moves: 1 (placed 1, dropped 0, carried away 0, removed 0, interrupted 0, partial 0, carried 1), mean 4.0 s, 0.84 units
push bursts: 6 (left 4, right 1, both 1), pushes 22
events: new 82, confirm 66, freeze 37, push 22, lift 1, hinge 1, carry 1, place 1, drop 0, gone 0, delete 66, split 0, other 0
trays: 2 (arrived 1), stones at arrival 33, added 0 (+0 unclear), removed 0 (+0 unclear), not sure 5
left hand: free 0:20.0, touch 0:08.8, grasp 0:00.0, hinge 0:00.0, carry 0:01.4
right hand: free 0:22.6, touch 0:03.2, grasp 0:00.4, hinge 0:00.2, carry 0:03.8
```

(the fixture recording aug28-1270-1300, `fixtures/golden/aug28-1270-1300.summary.txt`)

Its JSON form (`json.Marshal(summary)`) has exactly the keys of the contract, numbers rounded to 3 decimals and times
in seconds, so it can be compared with the published summaries of the complete recordings.

## Numbers in text

The sentences never use Go's float formatting, which rounds ties differently from the other languages. `FormatFixed(x, d)`
computes `floor(x × 10^d + 0.5)` in doubles and places the decimal point (`FormatFixed(0.725, 2)` is `0.73`, where
`%.2f` prints `0.72`);
`FormatClock(t)` is the clock of a media time; `Round3(x)` rounds as the JSON summary does. Use them when you print
numbers that should match the SDK's text.

## Feeding frames yourself

`InterpreterCore` is the interpreter's pure, synchronous state machine: `Feed(frame)` returns the frame's `*Update`,
`End()` closes what is still open and returns it as the final update (`Finish()` returns just its activities),
`State()`, `Summary()`, `Track(id)` and `Trays()` read it, `KeepTrays(n)` sets how many trays that have left it keeps
(the newest; 0 keeps every one), `SetClock` gives it the session clock for wall times, and `Gap()` tells it that the
stream dropped frames (a `gap` or `reset` event). It does no I/O and starts no goroutines, so you can feed it frames from anywhere: ones
you stored, or ones you read with `GetFrame`. The `Interpreter` and `Replay` are built on it.

```go
core := pdiggerfactory.NewInterpreterCore()
events := 0
for frame, err := range replay.Frames(ctx) {
	if err != nil {
		log.Fatal(err)
	}
	update := core.Feed(frame) // synchronous: no I/O, no goroutines
	events += len(update.Events)
}
state := core.State()
```

(from [examples/inventory](../examples/inventory/main.go), which prints the inventory of the last frame:
`dry2 at 10:00.1, after 192 events: 42 stones`, then `stone 21: rect, frozen, at (-0.11, 0.67)` and the others)

Feed frames of a session in order. A frame of an earlier grid index is treated as a revision (when its world has a
higher `world.rev`) or dropped as a duplicate (`Update.Duplicate`). A core is not safe for concurrent use.
