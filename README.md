# lifx-maestro

lifx-maestro is a local-first desktop app for generating and playing synchronized smart-light choreographies from music.
It ships as a Wails GUI, with `maestro` as an equivalent CLI for scripting and development.

The current implementation can:

- analyze MP3/WAV audio, with no Python install needed in released builds
- estimate tempo, beats, energy, and rough song sections
- generate deterministic timeline JSON
- play timelines against LIFX LAN devices
- perform audio and lighting together from the same audio clock
- discover device capabilities for single-zone, multizone, and matrix devices
- render generated effects into whole-device, zone, or matrix timeline actions
- run in dry-run mode without touching real lights
- restore the initial light state when playback exits

The desktop app adds device discovery, a timeline editor with per-event colors, and audio-synchronized preview with pause and resume.

The project intentionally does not include AI generation, waveform rendering, or live visualization yet.

## Setup

Install Go dependencies:

```bash
go mod tidy
```

Install Python audio-analysis dependencies:

```bash
python3 -m venv analyzer/.venv
analyzer/.venv/bin/python -m pip install -r analyzer/requirements.txt
```

Development builds automatically prefer `analyzer/.venv/bin/python` when it exists, resolved relative to the source tree rather than the working directory. You can still override the executable with `--python`.

Released builds do not need any of this: see [Bundled analyzer](#bundled-analyzer).

If your Python is externally managed and you prefer a repo-root venv, that is also detected:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r analyzer/requirements.txt
```

Override explicitly when needed:

```bash
go run ./cmd/maestro analyze samples/song.mp3 --python .venv/bin/python
```

## Bundled analyzer

Release builds ship the audio analyzer as a self-contained executable, so
downloaded apps analyze audio with no Python install and no `pip install` step.

- Release CI freezes `analyzer/analyze.py` with PyInstaller, smoke tests it, and
  writes the bundle to `internal/analyzerbin/assets/analyzer.zip`.
- `internal/analyzerbin` embeds that archive and extracts it to
  `~/.lifx-maestro/analyzer` on first launch, re-extracting whenever
  `analyzerbin.Version` changes.
- The repository commits a 22-byte placeholder archive in its place, so
  development builds stay small and fall back to a local Python interpreter plus
  `analyzer/analyze.py`.

Bump `Version` in `internal/analyzerbin/version.go` whenever `analyze.py` or its
dependencies change, otherwise existing installs keep their extracted copy.

To reproduce a bundled build locally, which is the only way to test the packaged
app without cutting a release:

```bash
analyzer/.venv/bin/python -m pip install pyinstaller
scripts/bundle-analyzer.sh          # freeze, smoke test, embed
wails build                          # package the app
git checkout internal/analyzerbin/assets/analyzer.zip   # restore the placeholder
```

Run the script rather than the steps by hand. Freezing without embedding leaves the
placeholder in place, and the app then falls back to a local Python interpreter: it
behaves like a development build while appearing to work, because a venv is present.

The tell is the first-run walkthrough. It only shows when a real analyzer is
bundled, so if a supposedly packaged build shows no walkthrough, it is running the
placeholder.

## Quick Start

Dry-run an existing timeline:

```bash
go run ./cmd/maestro play projects/demo.json --dry-run --verbose
```

Generate a choreography:

```bash
go run ./cmd/maestro generate samples/song.mp3 --output projects/song.json --style synthwave
```

Play a generated timeline on LIFX devices:

```bash
go run ./cmd/maestro play projects/song.json
```

Analyze, generate, play audio, and control lights in one command:

```bash
go run ./cmd/maestro perform samples/song.mp3 --style cinematic --target all
```

Test the full perform flow without touching real lights:

```bash
go run ./cmd/maestro perform samples/song.mp3 --dry-run --verbose
```

List discovered devices and capabilities:

```bash
go run ./cmd/maestro devices
```

## Commands

### `maestro analyze`

Analyze an audio file and print analysis JSON.

```bash
go run ./cmd/maestro analyze <song.mp3|song.wav>
```

Options:

- `--python string`: Python executable to use instead of the bundled analyzer. Default: the bundled analyzer in released builds, otherwise `analyzer/.venv/bin/python`, then `.venv/bin/python`, then `python3`

Example:

```bash
go run ./cmd/maestro analyze samples/song.mp3 --python .venv/bin/python
```

Output includes:

- `duration_ms`
- `bpm`
- `beats`
- `energy`
- `sections`

### `maestro generate`

Analyze audio and write a generated timeline JSON file.

```bash
go run ./cmd/maestro generate <song.mp3|song.wav>
```

Options:

- `--output string`: timeline JSON output path. If omitted, writes to `projects/<song-name>.json`
- `--style string`: generation style
- `--target string`: timeline target selector. Default: `all`
- `--python string`: Python executable to use instead of the bundled analyzer. Default: the bundled analyzer in released builds, otherwise `analyzer/.venv/bin/python`, then `.venv/bin/python`, then `python3`

Examples:

```bash
go run ./cmd/maestro generate samples/song.mp3 --output projects/song.json
go run ./cmd/maestro generate samples/song.mp3 --style neon --target desk
go run ./cmd/maestro generate samples/song.mp3 --style minimal --python .venv/bin/python
```

### `maestro perform`

Analyze audio, generate a choreography, start audio playback, and dispatch lighting events synchronized to the audio clock.

```bash
go run ./cmd/maestro perform <song.mp3>
```

Options:

- `--dry-run`: use the mock controller instead of real LIFX devices
- `--verbose`: print analysis, scheduler, and playback logs
- `--style string`: generation style
- `--target string`: target selector. Default: `all`
- `--python string`: Python executable to use instead of the bundled analyzer. Default: the bundled analyzer in released builds, otherwise `analyzer/.venv/bin/python`, then `.venv/bin/python`, then `python3`

Examples:

```bash
go run ./cmd/maestro perform samples/song.mp3 --dry-run --verbose
go run ./cmd/maestro perform samples/song.mp3 --style synthwave --target all
go run ./cmd/maestro perform samples/song.mp3 --style cinematic --target desk
go run ./cmd/maestro perform samples/song.mp3 --style neon --target tv,desk
```

During `perform`, audio playback owns the master clock. The lighting scheduler follows the audio position rather than using an independent wall-clock timer.

You do not need to run `generate` before `perform`. `perform` analyzes and generates an in-memory timeline for the current run. Use `generate` when you want to inspect, save, or replay a timeline separately with `play`.

### `maestro devices`

Discover LIFX devices and print their capabilities.

```bash
go run ./cmd/maestro devices
```

Options:

- `--dry-run`: print mock single-zone, multizone, and matrix devices

Example output:

```text
Desk Lamp          single_zone color
Light Strip        multi_zone  16 zones
Tile               matrix      8x8 x2
```

### `maestro play`

Play an existing timeline JSON file.

```bash
go run ./cmd/maestro play <timeline.json>
```

Options:

- `--dry-run`: use the mock controller instead of real LIFX devices
- `--verbose`: print timeline and scheduler details

Examples:

```bash
go run ./cmd/maestro play projects/demo.json --dry-run --verbose
go run ./cmd/maestro play projects/song.json
```

### `maestro styles`

List available generation styles.

```bash
go run ./cmd/maestro styles
```

Current styles:

- `cinematic`
- `cyberpunk`
- `minimal`
- `neon`
- `synthwave`
- `warm`

## Styles and Generation

The generator is section-aware. It uses rough song sections from the analyzer:

- intro
- build
- drop
- breakdown
- outro

Each section maps to reusable lighting primitives such as breathing, alternating pulse, fade, and sweep. Effects describe lighting intent first, then renderers translate that intent for each device kind.

Styles influence:

- palette
- per-device color distribution
- brightness scale
- transition aggressiveness
- pulse density
- timing offsets

If `--style` is omitted, generation defaults to `synthwave`.

## Device Targets

Common target values:

- `all`
- a LIFX device label, for example `desk`
- a LIFX group name
- a LIFX location name
- a 12-character LIFX serial
- a comma-separated list of selectors, for example `tv,desk`

For generated timelines, `--target` sets the target written into the JSON:

```bash
go run ./cmd/maestro generate samples/song.mp3 --target desk
```

For live performance, `--target` selects targets:

```bash
go run ./cmd/maestro perform samples/song.mp3 --target all
go run ./cmd/maestro perform samples/song.mp3 --target tv,desk
```

When using real LIFX devices, the controller discovers devices on the local network and exposes basic capabilities:

- single-zone
- multizone
- matrix

The generator uses those capabilities when they are available. Single-zone devices receive varied whole-device colors, multizone devices can receive zone color arrays, and matrix devices can receive pixel color arrays. Matrix chains currently replicate the same rendered frame across every tile in the chain. Unsupported combinations fall back to whole-device behavior.

## Restore on Exit

Before `play` and `perform` start, lifx-maestro captures the initial state of the target lights.

On normal completion, Ctrl-C, or error, it attempts to restore:

- power state
- hue
- saturation
- brightness
- kelvin

Restore is best-effort. If restoration fails, the error is logged without hiding the original playback error.

Dry-run mode logs capture and restore operations:

```text
[00:00.000] all -> capture_state
...
[00:05.000] all -> restore_state
```

## Timeline Format

Generated timelines are JSON files.

Example:

```json
{
  "name": "demo",
  "duration_ms": 10000,
  "events": [
    {
      "time_ms": 0,
      "target": "all",
      "action": "power_on"
    },
    {
      "time_ms": 1000,
      "target": "desk",
      "action": "set_color",
      "params": {
        "hue": 220,
        "saturation": 1.0,
        "brightness": 0.8,
        "kelvin": 3500,
        "duration_ms": 150
      }
    }
  ]
}
```

Supported actions currently:

- `power_on`
- `power_off`
- `set_color`
- `set_zone_colors`
- `set_matrix_colors`

Example multizone event:

```json
{
  "time_ms": 2400,
  "target": "strip",
  "action": "set_zone_colors",
  "params": {
    "duration_ms": 120,
    "zones": [
      {
        "index": 0,
        "color": {
          "hue": 235,
          "saturation": 0.55,
          "brightness": 0.7,
          "kelvin": 3600
        }
      }
    ]
  }
}
```

Example matrix event:

```json
{
  "time_ms": 2400,
  "target": "tile",
  "action": "set_matrix_colors",
  "params": {
    "width": 8,
    "height": 8,
    "duration_ms": 120,
    "pixels": [
      {
        "x": 0,
        "y": 0,
        "color": {
          "hue": 285,
          "saturation": 1.0,
          "brightness": 0.7,
          "kelvin": 3500
        }
      }
    ]
  }
}
```

## Development Checks

Run Go tests:

```bash
go test ./...
```

Preview the first-run walkthrough, which normally only shows while the bundled
analyzer prepares itself and so never appears in a development build:

```bash
LIFX_MAESTRO_FORCE_TOUR=1 wails dev
```

Check the Python analyzer syntax:

```bash
python3 -m py_compile analyzer/analyze.py
```

## Current Limitations

- Audio playback in `perform` currently supports MP3.
- Audio analysis in development builds depends on Python packages from `analyzer/requirements.txt`. Released builds use the bundled analyzer instead.
- Section detection is heuristic and approximate.
- Multizone and matrix rendering is intentionally simple: gradients, sweeps, pulses, and full color arrays only.
- No AI generation or waveform view yet.
- Released macOS and Windows builds are unsigned, so the OS warns on first launch.
