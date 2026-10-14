# Embedded Live

Import `github.com/alessio-palumbo/lifx-maestro/live` and configure `live.New`
with an audio source, analyzer, explicit targets, and output sink. Run it with a
cancelable context. See `examples/live` for a runnable example without hardware:

```sh
go run ./examples/live
```

The same rolling engine, state tracker, and generator power the CLI and desktop
application. The public engine never discovers lights, opens or closes a
controller, captures or restores state, or changes device power. The host owns
those decisions and provides target IDs with `device.SurfaceFromDevice` geometry.

Each output batch contains one **logical** `effects.Frame` per target, covering
the entire surface. Pass it to the host's `lifxlan-go` renderer's `RenderFrame`.
That renderer adapts all matrix chains, handles hidden cells, and applies device
orientation. Do not pre-adapt or pre-rotate these frames. The older standalone
timeline conversion uses `deviceFrames[0]` and broadcasts that physical frame to
chains; embedded output bypasses that conversion and retains the full logical
surface. Standalone timeline behavior is preserved for this milestone.

`Engine.Run` is single-use. Cancel its context and wait for it to return before
restoring lights or switching output ownership. A sink must honor cancellation
and finish all its writes before `Write` returns; the engine waits for capture
and sink completion. Normal cancellation returns nil; capture, analysis, and
output failures are returned with their cause available to `errors.Is`.
The engine does not close the supplied analyzer or sink. Close them after Run.
The audio source owns its capture resources and releases them before its Run
returns. It must honor cancellation, transfer ownership of PCM slices, and leave
the output channel open (the engine closes it).

Capture has an eight-chunk bounded queue. Output has a single pending batch;
newer batches replace pending older ones when the sink is slow. Finite sources
drain the latest pending output; cancellation drops pending output. Observers
run on the analysis loop and must return promptly. `OutputObserver` additionally
reports generation and replacement counts (the existing `GeneratedEvents` and
`DroppedEvents` fields count target frames in embedded mode).

`SetStyle` and `SetDynamics` apply to future frames while running. Dynamics
accepts `auto`, `calm`, `balanced`, or `energetic`. Microphone sensitivity can be
configured with `TrackerConfigForSensitivity`. Defaults retain the existing
500 ms overlapping analysis window and 100 ms hop.

Optional integrations:

- `live/microphone`: native capture and input enumeration using malgo. Importing
  the core does not require CGO or native audio libraries.
- `live/python`: persistent analyzer process, accepting an explicit executable
  or Python interpreter plus script. Supply absolute paths. This helper does
  not extract the bundled analyzer. Cancellation during analysis terminates
  that process; create a new analyzer for the next run and close the old one.

The host may use its own implementations instead. No Wails dependency is needed
for the core API. Real microphone/audio analysis and physical LAN behavior need
host integration testing; the external-package tests use only injected fakes.
