// Package pdiggerfactory is the Go SDK of the pDigger Factory interpreter API: it follows the stone work a factory
// camera sees (who lifts, carries, places or pushes which stone, and where every stone is) and turns it into one
// report per tray (the stones taken off, put on and moved, when and where, and whether that is the tray's whole account) and,
// underneath, typed events, activities and plain sentences such as "21:17.8 Right hand lifts stone 2660".
//
// The SDK has two modes and a testing package. Simple mode is one [TrayReport] per tray: [Tray.Report] once a
// tray has left, [Interpreter.TrayReports] from a unit as each tray leaves, [Replay.TrayReports] for every tray of a
// recording; [TrayReport.Describe] writes it as text, and json.Marshal as data. Advanced mode is the detail
// underneath, for research and for the moments a report is not complete, in three layers:
//
//   - High level: [Interpreter] follows the shared session (a recording, or one of the plant's live cameras) and yields
//     [Update], [Event] and [Activity] values with Describe methods, and [TrayFact] values about each [Tray], whose
//     [TrayStone] ids are the SDK's own, one per stone and never reused; [Replay] does the same offline over a
//     recording file (one of the four published recordings, or any file in the recording format pfr1). Session and
//     source helpers live on [Client.Session], [Client.Sources], [Client.Cameras] and [Client.Live]; [Session.UntilLive]
//     waits out a camera's warm-up and [Session.Picture] gives a live frame's own picture.
//   - Mid level: [Streams] delivers frames over server-sent events with automatic resume (Last-Event-ID), late-layer
//     backfill, revisions and duplicate dropping; [Streams.Poll] is the long-poll equivalent and [Streams.RawPackets]
//     the raw model codec stream. The render-ready draw lists ([Overlay2D], [Scene3D]) come from [API].
//   - Low level: [API] has one method per HTTP operation of the OpenAPI document (spec/openapi.yaml), with typed
//     models for every schema and one error type, [Error]. Package codec decodes maps, bitsets, depth, surface
//     buffers, joints and stone tuples.
//   - Testing: package factorytest is a fake server and transport that answers from fixture replies and recordings,
//     so code that uses the SDK can be unit-tested without a unit on the LAN.
//
// Async model: every call takes a [context.Context]; streams are range-over-func iterators ([iter.Seq2]) that stop
// and close their HTTP connection when the loop breaks or the context is cancelled. [Chan] turns any stream into a
// channel for select-based code.
//
// Scene coordinates are in the template gauge (the scale of a template adult), not metres; the SDK reports
// distances in "units" for that reason.
package pdiggerfactory
