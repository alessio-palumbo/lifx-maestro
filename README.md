# lifx-maestro

lifx-maestro is a local-first CLI for generating and playing synchronized smart-light choreographies from music.

The current implementation can:

- analyze MP3/WAV audio with a Python helper
- estimate tempo, beats, energy, and rough song sections
- generate deterministic timeline JSON
- play timelines against LIFX LAN devices
- perform audio and lighting together from the same audio clock
- discover device capabilities for single-zone, multizone, and matrix devices
- render generated effects into whole-device, zone, or matrix timeline actions
- run in dry-run mode without touching real lights
- restore the initial light state when playback exits

The project intentionally does not include a GUI, AI generation, waveform rendering, or live visualization yet.

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

The CLI automatically prefers `analyzer/.venv/bin/python` when it exists. You can still override the executable with `--python`.

If your Python is externally managed and you prefer a repo-root venv, that is also detected:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r analyzer/requirements.txt
```

Override explicitly when needed:

```bash
go run ./cmd/maestro analyze samples/song.mp3 --python .venv/bin/python
```

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

- `--python string`: Python executable to use. Default: `analyzer/.venv/bin/python` when present, then `.venv/bin/python`, then `python3`

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
- `--python string`: Python executable. Default: `analyzer/.venv/bin/python` when present, then `.venv/bin/python`, then `python3`

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
- `--python string`: Python executable. Default: `analyzer/.venv/bin/python` when present, then `.venv/bin/python`, then `python3`

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
Tile               matrix      8x8
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

The generator uses those capabilities when they are available. Single-zone devices receive varied whole-device colors, multizone devices can receive zone color arrays, and matrix devices can receive pixel color arrays. Unsupported combinations fall back to whole-device behavior.

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

Check the Python analyzer syntax:

```bash
python3 -m py_compile analyzer/analyze.py
```

## Current Limitations

- Audio playback in `perform` currently supports MP3.
- Audio analysis depends on Python packages from `analyzer/requirements.txt`.
- Section detection is heuristic and approximate.
- Multizone and matrix rendering is intentionally simple: gradients, sweeps, pulses, and full color arrays only.
- No GUI, AI generation, waveform view, or timeline editor yet.
