#!/usr/bin/env python3
import json
import sys

import librosa
import numpy as np
import soundfile as sf


def main():
    if len(sys.argv) != 2:
        print("usage: analyze.py <audio-file>", file=sys.stderr)
        return 2

    path = sys.argv[1]
    y, sr = librosa.load(path, sr=None, mono=True)
    duration_ms = duration_ms_for(path, y, sr)

    onset_env = librosa.onset.onset_strength(y=y, sr=sr)
    tempo, beat_frames = librosa.beat.beat_track(y=y, sr=sr, onset_envelope=onset_env)
    tempo = float(np.asarray(tempo).reshape(-1)[0]) if np.size(tempo) else 0.0
    beat_times = librosa.frames_to_time(beat_frames, sr=sr)
    beats = [int(round(t * 1000)) for t in beat_times if t >= 0]

    rms = librosa.feature.rms(y=y, frame_length=2048, hop_length=512)[0]
    rms_times = librosa.frames_to_time(np.arange(len(rms)), sr=sr, hop_length=512)
    energy = downsample_energy(rms_times, normalize(rms), step_ms=500)

    result = {
        "duration_ms": duration_ms,
        "bpm": round(tempo, 3),
        "beats": beats,
        "energy": energy,
        "sections": sections(duration_ms, energy),
    }
    print(json.dumps(result, separators=(",", ":")))
    return 0


def normalize(values):
    if len(values) == 0:
        return values
    max_value = float(np.max(values))
    if max_value <= 0:
        return np.zeros_like(values)
    return values / max_value


def duration_ms_for(path, y, sr):
    try:
        info = sf.info(path)
        return int(round((info.frames / info.samplerate) * 1000))
    except Exception:
        return int(round(librosa.get_duration(y=y, sr=sr) * 1000))


def downsample_energy(times, values, step_ms):
    if len(times) == 0 or len(values) == 0:
        return []

    points = []
    next_ms = 0
    bucket = []

    for time_s, value in zip(times, values):
        time_ms = int(round(float(time_s) * 1000))
        while time_ms >= next_ms + step_ms:
            if bucket:
                points.append({"time_ms": next_ms, "value": round(float(np.mean(bucket)), 4)})
                bucket = []
            next_ms += step_ms
        bucket.append(float(value))

    if bucket:
        points.append({"time_ms": next_ms, "value": round(float(np.mean(bucket)), 4)})

    return points


def sections(duration_ms, energy):
    if duration_ms <= 0:
        return []

    boundaries = [
        (0.00, 0.16, "intro"),
        (0.16, 0.36, "build"),
        (0.36, 0.68, "drop"),
        (0.68, 0.86, "breakdown"),
        (0.86, 1.00, "outro"),
    ]
    result = []
    for start_ratio, end_ratio, label in boundaries:
        start_ms = int(round(duration_ms * start_ratio))
        end_ms = int(round(duration_ms * end_ratio))
        result.append({
            "start_ms": start_ms,
            "end_ms": end_ms,
            "type": label,
            "energy": round(mean_energy(energy, start_ms, end_ms), 4),
        })
    return result


def mean_energy(energy, start_ms, end_ms):
    values = [p["value"] for p in energy if start_ms <= p["time_ms"] < end_ms]
    if not values:
        return 0.5
    return float(np.mean(values))


if __name__ == "__main__":
    raise SystemExit(main())
