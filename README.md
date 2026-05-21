# lifx-maestro

lifx-maestro is a local-first CLI for generating and playing synchronized smart-light choreographies from music.

The current implementation can:

- analyze MP3/WAV audio with a Python helper
- estimate tempo, beats, energy, and rough song sections
- generate deterministic timeline JSON
- play timelines against LIFX LAN devices
- perform audio and lighting together from the same audio clock
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
pip install -r python/requirements.txt
```

If your Python is externally managed, use a virtual environment:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r python/requirements.txt
```

Then pass the venv Python to commands that analyze audio:

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
go run ./cmd/maestro perform samples/song.mp3 --style cinematic --devices all
```

Test the full perform flow without touching real lights:

```bash
go run ./cmd/maestro perform samples/song.mp3 --dry-run --verbose
```

## Commands

### `maestro analyze`

Analyze an audio file and print analysis JSON.

```bash
go run ./cmd/maestro analyze <song.mp3|song.wav>
```

Options:

- `--python string`: Python executable to use. Default: `python3`

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
- `--python string`: Python executable. Default: `python3`

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
- `--devices string`: device selector. Default: `all`
- `--python string`: Python executable. Default: `python3`

Examples:

```bash
go run ./cmd/maestro perform samples/song.mp3 --dry-run --verbose
go run ./cmd/maestro perform samples/song.mp3 --style synthwave --devices all
go run ./cmd/maestro perform samples/song.mp3 --style cinematic --devices desk
```

During `perform`, audio playback owns the master clock. The lighting scheduler follows the audio position rather than using an independent wall-clock timer.

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

Each section maps to reusable lighting primitives such as breathing, alternating pulse, fade, and sweep-style whole-device fallback behavior.

Styles influence:

- palette
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

For generated timelines, `--target` sets the target written into the JSON:

```bash
go run ./cmd/maestro generate samples/song.mp3 --target desk
```

For live performance, `--devices` selects devices:

```bash
go run ./cmd/maestro perform samples/song.mp3 --devices all
```

When using real LIFX devices, the controller discovers devices on the local network and exposes basic capabilities:

- single-zone
- multizone
- matrix

Advanced spatial rendering is not implemented yet. Multizone and matrix devices currently fall back to whole-device behavior.

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

## Development Checks

Run Go tests:

```bash
go test ./...
```

Check the Python analyzer syntax:

```bash
python3 -m py_compile python/analyze.py
```

## Current Limitations

- Audio playback in `perform` currently supports MP3.
- Audio analysis depends on Python packages from `python/requirements.txt`.
- Section detection is heuristic and approximate.
- Multizone and matrix devices use whole-device fallback behavior.
- No GUI, AI generation, waveform view, or timeline editor yet.
