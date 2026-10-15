# Native streaming analysis

This experimental package contains audio DSP only: mono float32 PCM windows in,
RMS/band levels, onset strength, and tempo candidates out. It imports the standard
library and Gonum's real FFT. No Maestro, LIFX, capture, UI, or process types enter
its API. `live/native` is the small adapter to Maestro's public analyzer contract.
This boundary is intended to make later extraction into a separate module simple.

The initial implementation preserves `LiveWindowAnalyzer` in `analyzer/analyze.py`:

- A Hann-window real FFT measures low (20-250 Hz), mid (250-4000 Hz), and high
  (4000 Hz up to 12000 Hz or Nyquist) energy with the reference's Parseval scaling.
- Recent measurements use the final 100 ms; full measurements use the supplied
  window (normally 500 ms). The enclosing Live engine owns window assembly.
- 25 ms onset frames cover only newly arrived audio. Relative positive spectral
  flux, adaptive median/deviation thresholds, hysteresis, and a 250 ms cooldown
  retain the Python detector's behavior.
- An eight-second onset history supplies normalized autocorrelation over
  55-190 BPM lags, interpolated tempo, confidence, and up to five ranked candidates.
  The existing Go tracker still learns noise floors, gates activity, stabilizes
  tempo, and chooses dynamics.
- Numeric outputs retain Python's rounding. Timestamp subtraction also matches
  Python floating-point arithmetic because it affects cooldown boundary decisions.

History holds at most 320 onset frames. Three FFT workspaces retain their current
sizes; caller PCM slices are not retained. Cancellation is checked before work and
between onset frames and correlation lags. FFT calls finish before cancellation is
observed. Create one analyzer per stream; sample-rate changes require a new instance.

Run DSP tests and a benchmark without Python:

```sh
go test ./internal/audiodsp ./live/native
go test ./internal/audiodsp -run '^$' -bench BenchmarkAnalyze16000 -benchmem
```

Optional Python parity tests use identical synthetic PCM at 16 kHz and 44.1 kHz,
including silence, melody, fast transients, cooldown boundaries, and late timestamps.
They compare features, tracker/generator decisions, and timeline events for synthetic
single-zone, multizone, and matrix targets:

```sh
MAESTRO_TEST_PYTHON="$PWD/analyzer/.venv/bin/python" go test ./live/native -run TestPythonParity -v
```

Use `maestro evaluate-live --compare-native` for real songs. Reported analysis time
includes the first Analyze call (which can include delayed Python imports), but
excludes decoding, process creation, tracker work, and generation. Zero differences
on tested fixtures do not prove universal equivalence: floating-point FFT behavior
and tied tempo rankings can differ on other inputs. Keep Python as the reference
until broader microphone and music testing is satisfactory. Improving tempo is a
separate change from this parity migration.
