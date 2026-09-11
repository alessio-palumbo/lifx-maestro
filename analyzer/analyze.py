#!/usr/bin/env python3
import json
import struct
import sys
from collections import deque

import librosa
import numpy as np
import soundfile as sf


def main():
    if len(sys.argv) == 2 and sys.argv[1] == "--live":
        return live_main()

    if len(sys.argv) != 2:
        print("usage: analyze.py <audio-file> | --live", file=sys.stderr)
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
    energy = normalize_energy_points(downsample_energy(rms_times, rms, step_ms=500))

    result = {
        "duration_ms": duration_ms,
        "bpm": round(tempo, 3),
        "beats": beats,
        "energy": energy,
        "dynamics": track_dynamics(y, sr, onset_env),
        "sections": sections(y, sr, duration_ms, energy, onset_env, tempo),
        "streams": streams(y, sr, energy, onset_env, beats),
    }
    print(json.dumps(result, separators=(",", ":")))
    return 0


LIVE_HEADER = struct.Struct("<IIq")


def live_main():
    analyzer = LiveWindowAnalyzer()
    source = sys.stdin.buffer
    while True:
        header = read_exact(source, LIVE_HEADER.size)
        if not header:
            return 0
        sample_rate, sample_count, end_ns = LIVE_HEADER.unpack(header)
        payload = read_exact(source, sample_count * 4)
        if payload is None:
            raise EOFError("incomplete live PCM window")
        samples = np.frombuffer(payload, dtype="<f4")
        result = analyzer.analyze(samples, sample_rate, end_ns / 1_000_000_000.0)
        print(json.dumps(result, separators=(",", ":")), flush=True)


def read_exact(source, size):
    data = bytearray()
    while len(data) < size:
        chunk = source.read(size - len(data))
        if not chunk:
            return None if data else b""
        data.extend(chunk)
    return bytes(data)


class LiveWindowAnalyzer:
    """Stateful, causal analysis for overlapping microphone windows."""

    def __init__(self):
        self.previous_magnitude = None
        self.flux_history = deque(maxlen=160)
        self.transient_times = deque(maxlen=32)
        self.tempo = 0.0

    def analyze(self, samples, sample_rate, at_seconds):
        if sample_rate <= 0 or len(samples) == 0:
            return self.empty_result(at_seconds)

        window = np.hanning(len(samples))
        windowed = samples * window
        magnitude = np.abs(np.fft.rfft(windowed))
        frequencies = np.fft.rfftfreq(len(samples), d=1.0 / sample_rate)
        window_gain = float(np.sqrt(np.mean(np.square(window))))
        rms = float(np.sqrt(np.mean(np.square(samples, dtype=np.float64))))

        flux = 0.0
        if self.previous_magnitude is not None and len(self.previous_magnitude) == len(magnitude):
            positive = np.maximum(magnitude - self.previous_magnitude, 0.0)
            flux = float(np.sum(positive) / max(np.sum(self.previous_magnitude), 1e-12))
        self.previous_magnitude = magnitude

        history = np.asarray(self.flux_history, dtype=np.float64)
        threshold = max(0.025, float(np.median(history) + np.std(history) * 2.2)) if len(history) >= 8 else 0.08
        transient = flux >= threshold
        self.flux_history.append(flux)
        accepted_transient = transient and (not self.transient_times or at_seconds - self.transient_times[-1] >= 0.12)
        if accepted_transient:
            self.transient_times.append(at_seconds)

        tempo, confidence = self.estimate_tempo()
        if tempo > 0:
            self.tempo = tempo if self.tempo <= 0 else self.tempo * 0.82 + tempo * 0.18

        return {
            "at_ms": int(round(at_seconds * 1000)),
            "rms_db": amplitude_db(rms),
            "low_db": band_db(magnitude, frequencies, 20, 250, window_gain),
            "mid_db": band_db(magnitude, frequencies, 250, 4000, window_gain),
            "high_db": band_db(magnitude, frequencies, 4000, min(12000, sample_rate / 2), window_gain),
            "onset_strength": round(flux, 6),
            "tempo_bpm": round(self.tempo, 3),
            "tempo_confidence": round(confidence, 4),
            "beat": bool(accepted_transient and confidence >= 0.35),
        }

    def estimate_tempo(self):
        if len(self.transient_times) < 4:
            return 0.0, 0.0
        intervals = np.diff(np.asarray(self.transient_times, dtype=np.float64))
        folded = []
        for interval in intervals:
            while interval < 0.25:
                interval *= 2
            while interval > 1.5:
                interval /= 2
            if 0.25 <= interval <= 1.5:
                folded.append(interval)
        if len(folded) < 3:
            return 0.0, 0.0
        median = float(np.median(folded))
        deviations = np.abs(np.asarray(folded) - median)
        confidence = float(np.mean(deviations <= max(0.045, median * 0.12)))
        return 60.0 / median, confidence

    def empty_result(self, at_seconds):
        return {
            "at_ms": int(round(at_seconds * 1000)),
            "rms_db": -120.0,
            "low_db": -120.0,
            "mid_db": -120.0,
            "high_db": -120.0,
            "onset_strength": 0.0,
            "tempo_bpm": self.tempo,
            "tempo_confidence": 0.0,
            "beat": False,
        }


def amplitude_db(value):
    return round(20.0 * np.log10(max(float(value), 1e-6)), 4)


def band_db(magnitude, frequencies, low_hz, high_hz, window_gain):
    mask = (frequencies >= low_hz) & (frequencies < high_hz)
    if not np.any(mask):
        return -120.0
    # Parseval scaling keeps a tone's band amplitude comparable with full-window
    # RMS. The Go tracker still learns an independent ambient floor per band.
    sample_count = max((len(magnitude) - 1) * 2, 1)
    mean_square = 2.0 * float(np.sum(np.square(magnitude[mask], dtype=np.float64))) / (sample_count * sample_count)
    return amplitude_db(np.sqrt(mean_square) / max(window_gain, 1e-6))


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
                points.append({"time_ms": next_ms, "value": float(np.mean(bucket))})
                bucket = []
            next_ms += step_ms
        bucket.append(float(value))

    if bucket:
        points.append({"time_ms": next_ms, "value": round(float(np.mean(bucket)), 4)})

    return points


def normalize_energy_points(points):
    """Scale bucket energies so the loudest bucket reads 1.0.

    Normalising per frame before averaging leaves the curve well short of 1.0,
    because a half-second average is always quieter than the single loudest frame
    inside it, and by how much depends on the track. Consumers treat these values
    as a 0-1 loudness, so the scaling has to happen after the averaging.
    """
    if not points:
        return points

    peak = max(point["value"] for point in points)
    for point in points:
        point["value"] = round(point["value"] / peak, 4) if peak > 0 else 0.0
    return points


def track_dynamics(y, sr, onset_env):
    """Describe the track's overall force without guessing a genre.

    Note density is deliberately the lightest-weight component: fast piano has
    many onsets, but its lower loudness, softer transients and darker spectrum
    should still produce a calm result.
    """
    if len(y) == 0:
        return {
            "profile": "balanced",
            "intensity": 0.5,
            "loudness": 0.5,
            "transient_strength": 0.5,
            "spectral_brightness": 0.5,
            "dynamic_contrast": 0.5,
            "onset_activity": 0.5,
        }

    hop_length = 512
    rms = librosa.feature.rms(y=y, frame_length=2048, hop_length=hop_length)[0]
    rms_db = librosa.amplitude_to_db(np.maximum(rms, 1e-10), ref=1.0)
    mean_rms = max(float(np.sqrt(np.mean(y * y))), 1e-10)
    mean_rms_db = 20 * np.log10(mean_rms)
    loudness = unit_scale(mean_rms_db, -24.0, -8.0)

    transient_p90 = float(np.percentile(onset_env, 90)) if len(onset_env) else 0.0
    transient_strength = unit_scale(transient_p90, 0.8, 2.8)

    centroid = librosa.feature.spectral_centroid(y=y, sr=sr, hop_length=hop_length)[0]
    median_centroid = float(np.median(centroid)) if len(centroid) else 0.0
    spectral_brightness = unit_scale(median_centroid, 400.0, 4500.0)

    dynamic_spread_db = float(np.percentile(rms_db, 90) - np.percentile(rms_db, 10)) if len(rms_db) else 0.0
    dynamic_contrast = unit_scale(dynamic_spread_db, 4.0, 14.0)

    onset_times = librosa.onset.onset_detect(
        onset_envelope=onset_env,
        sr=sr,
        hop_length=hop_length,
        units="time",
    )
    duration_s = max(float(len(y)) / float(sr), 0.001)
    onset_activity = unit_scale(float(len(onset_times)) / duration_s, 1.5, 6.5)

    intensity = (
        loudness * 0.32
        + transient_strength * 0.26
        + spectral_brightness * 0.16
        + dynamic_contrast * 0.14
        + onset_activity * 0.12
    )
    if intensity < 0.38:
        profile = "calm"
    elif intensity >= 0.72:
        profile = "energetic"
    else:
        profile = "balanced"

    return {
        "profile": profile,
        "intensity": round(float(intensity), 4),
        "loudness": round(loudness, 4),
        "transient_strength": round(transient_strength, 4),
        "spectral_brightness": round(spectral_brightness, 4),
        "dynamic_contrast": round(dynamic_contrast, 4),
        "onset_activity": round(onset_activity, 4),
    }


def unit_scale(value, low, high):
    if high <= low:
        return 0.0
    return float(np.clip((value - low) / (high - low), 0.0, 1.0))


def streams(y, sr, full_energy, full_onset_env, beats):
    hop_length = 512
    frame_length = 2048
    bands = [
        ("low", "Low (20-250 Hz)", 20, 250),
        ("mid", "Mid (250-4,000 Hz)", 250, 4000),
        ("high", "High (4,000-12,000 Hz)", 4000, min(12000, sr / 2)),
    ]

    result = []
    for stream_id, label, low_hz, high_hz in bands:
        band = band_limited_audio(y, sr, low_hz, high_hz)
        rms = librosa.feature.rms(y=band, frame_length=frame_length, hop_length=hop_length)[0]
        times = librosa.frames_to_time(np.arange(len(rms)), sr=sr, hop_length=hop_length)
        energy = normalize_energy_points(downsample_energy(times, rms, step_ms=500))
        onset_env = librosa.onset.onset_strength(y=band, sr=sr, hop_length=hop_length)
        result.append({
            "id": stream_id,
            "label": label,
            "energy": energy,
            "accents": accents_from_onsets(onset_env, sr, hop_length, min_gap_ms=120),
        })

    result.append({
        "id": "full",
        "label": "Full spectrum / accents",
        "energy": full_energy,
        "accents": accents_from_onsets(full_onset_env, sr, hop_length, fallback_beats=beats, min_gap_ms=180),
    })
    return result


def band_limited_audio(y, sr, low_hz, high_hz):
    if len(y) == 0:
        return y

    spectrum = np.fft.rfft(y)
    freqs = np.fft.rfftfreq(len(y), d=1.0 / sr)
    mask = (freqs >= low_hz) & (freqs < high_hz)
    filtered = np.fft.irfft(spectrum * mask, n=len(y))
    return filtered.astype(np.float32)


def accents_from_onsets(onset_env, sr, hop_length, fallback_beats=None, min_gap_ms=120):
    if len(onset_env) == 0:
        return fallback_beats or []

    normalized = normalize(np.asarray(onset_env))
    threshold = max(float(np.percentile(normalized, 72)), float(np.mean(normalized) + np.std(normalized) * 0.15))
    try:
        peaks = librosa.util.peak_pick(
            normalized,
            pre_max=3,
            post_max=3,
            pre_avg=8,
            post_avg=8,
            delta=max(0.03, threshold * 0.25),
            wait=3,
        )
    except Exception:
        peaks = np.array([], dtype=int)

    accents = []
    for frame in peaks:
        if normalized[frame] < threshold:
            continue
        time_s = librosa.frames_to_time(frame, sr=sr, hop_length=hop_length)
        accents.append(int(round(float(time_s) * 1000)))

    if not accents and fallback_beats:
        return fallback_beats
    return coalesce_accents(accents, min_gap_ms)


def coalesce_accents(accents, min_gap_ms):
    if not accents:
        return accents
    out = [accents[0]]
    last = accents[0]
    for accent in accents[1:]:
        if accent - last >= min_gap_ms:
            out.append(accent)
            last = accent
    return out


def sections(y, sr, duration_ms, energy, onset_env, tempo):
    detected = feature_sections(y, sr, duration_ms, energy, onset_env, tempo)
    if detected:
        return detected
    return fallback_sections(duration_ms, energy)


def feature_sections(y, sr, duration_ms, energy, onset_env, tempo):
    if duration_ms <= 0:
        return []

    try:
        features, times_ms = section_features(y, sr, onset_env)
        if len(times_ms) < 8:
            return []

        binned, bin_times = bin_features(features, times_ms, step_ms=1000)
        if len(bin_times) < 8:
            return []

        novelty = novelty_curve(binned)
        boundaries = pick_boundaries(novelty, bin_times, duration_ms)
        if len(boundaries) < 4:
            return []

        boundaries = insert_pre_drop_builds(boundaries, duration_ms, energy, tempo)
        return label_sections(boundaries, duration_ms, energy)
    except Exception:
        return []


def section_features(y, sr, onset_env):
    hop_length = 512
    rms = librosa.feature.rms(y=y, frame_length=2048, hop_length=hop_length)[0]
    centroid = librosa.feature.spectral_centroid(y=y, sr=sr, hop_length=hop_length)[0]
    bandwidth = librosa.feature.spectral_bandwidth(y=y, sr=sr, hop_length=hop_length)[0]
    rolloff = librosa.feature.spectral_rolloff(y=y, sr=sr, hop_length=hop_length)[0]
    zcr = librosa.feature.zero_crossing_rate(y, frame_length=2048, hop_length=hop_length)[0]

    min_len = min(len(rms), len(onset_env), len(centroid), len(bandwidth), len(rolloff), len(zcr))
    if min_len == 0:
        return np.empty((0, 0)), np.array([])

    stacked = np.vstack([
        normalize(rms[:min_len]),
        normalize(onset_env[:min_len]),
        normalize(centroid[:min_len]),
        normalize(bandwidth[:min_len]),
        normalize(rolloff[:min_len]),
        normalize(zcr[:min_len]),
    ]).T
    times_ms = librosa.frames_to_time(np.arange(min_len), sr=sr, hop_length=hop_length) * 1000
    return stacked, times_ms


def bin_features(features, times_ms, step_ms):
    if len(features) == 0:
        return np.empty((0, 0)), np.array([])

    bins = []
    bin_times = []
    next_ms = 0
    bucket = []

    for feature, time_ms in zip(features, times_ms):
        while time_ms >= next_ms + step_ms:
            if bucket:
                bins.append(np.mean(bucket, axis=0))
                bin_times.append(next_ms)
                bucket = []
            next_ms += step_ms
        bucket.append(feature)

    if bucket:
        bins.append(np.mean(bucket, axis=0))
        bin_times.append(next_ms)

    return np.asarray(bins), np.asarray(bin_times)


def novelty_curve(features):
    if len(features) < 2:
        return np.array([])

    smoothed = smooth_rows(features, width=3)
    delta = np.diff(smoothed, axis=0)
    novelty = np.linalg.norm(delta, axis=1)

    energy_delta = np.maximum(delta[:, 0], 0)
    onset_delta = np.maximum(delta[:, 1], 0)
    novelty = novelty + energy_delta * 0.75 + onset_delta * 0.5
    return normalize(novelty)


def smooth_rows(values, width):
    if len(values) < width:
        return values
    out = np.empty_like(values)
    radius = width // 2
    for i in range(len(values)):
        start = max(0, i - radius)
        end = min(len(values), i + radius + 1)
        out[i] = np.mean(values[start:end], axis=0)
    return out


def pick_boundaries(novelty, bin_times, duration_ms):
    if len(novelty) == 0:
        return []

    min_section_ms = min(8000, max(5000, int(duration_ms * 0.045)))
    edge_guard_ms = min(10000, max(4000, int(duration_ms * 0.035)))
    threshold = max(float(np.percentile(novelty, 68)), float(np.mean(novelty) + np.std(novelty) * 0.2))
    candidates = []

    for i, value in enumerate(novelty):
        time_ms = int(bin_times[i + 1]) if i + 1 < len(bin_times) else int(bin_times[i])
        if time_ms < edge_guard_ms or time_ms > duration_ms - edge_guard_ms:
            continue
        prev_value = novelty[i - 1] if i > 0 else -1
        next_value = novelty[i + 1] if i + 1 < len(novelty) else -1
        if value >= threshold and value >= prev_value and value >= next_value:
            candidates.append((time_ms, float(value)))

    if not candidates:
        return []

    selected = []
    for time_ms, score in sorted(candidates, key=lambda item: item[1], reverse=True):
        if all(abs(time_ms - existing) >= min_section_ms for existing in selected):
            selected.append(time_ms)
        if len(selected) >= 6:
            break

    selected.sort()
    return [0] + selected + [duration_ms]


def insert_pre_drop_builds(boundaries, duration_ms, energy, tempo):
    if len(boundaries) < 3:
        return boundaries

    beat_ms = 500
    if tempo > 0:
        beat_ms = int(round(60000 / tempo))
    build_window_ms = clamp_int(beat_ms * 16, 6000, 10000)

    # Gate on the track's own loud range for the same reason as label_sections.
    curve = [point["value"] for point in energy]
    if curve:
        loud_energy = float(np.percentile(curve, 70))
        rise, _ = relative_deltas(curve)
    else:
        loud_energy, rise = 1.0, 1.0

    out = [boundaries[0]]
    for i in range(1, len(boundaries) - 1):
        boundary = boundaries[i]
        previous = out[-1]
        next_boundary = boundaries[i + 1]

        before_energy = mean_energy(energy, max(previous, boundary - build_window_ms), boundary)
        after_energy = mean_energy(energy, boundary, min(next_boundary, boundary + build_window_ms))
        build_start = boundary - build_window_ms

        if (
            after_energy >= loud_energy
            and after_energy >= before_energy + rise
            and build_start - previous >= 5000
            and boundary - build_start >= 5000
        ):
            out.append(build_start)

        out.append(boundary)

    out.append(boundaries[-1])
    deduped = []
    for boundary in out:
        if not deduped or boundary != deduped[-1]:
            deduped.append(int(clamp_int(boundary, 0, duration_ms)))
    return deduped


def relative_deltas(values):
    """Return how much of a change counts as rising or falling for this track.

    Expressed as a fraction of the track's own spread so a narrow-range song
    still registers its swings, rather than being measured against a constant
    that only suits loud, dynamic material.
    """
    spread = float(np.percentile(values, 90) - np.percentile(values, 10))
    if spread <= 0:
        # A flat track has no swings to find. Return a delta nothing can clear so
        # the comparisons do not fire on equal values.
        return 1.0, 1.0
    return spread * 0.35, spread * 0.5


def label_sections(boundaries, duration_ms, energy):
    if len(boundaries) < 3:
        return []

    segment_energies = [
        mean_energy(energy, boundaries[i], boundaries[i + 1])
        for i in range(len(boundaries) - 1)
    ]
    # Thresholds follow the track's own dynamics. Absolute cutoffs collapsed
    # quiet and heavily compressed songs into a single label, because the scale of
    # the energy curve depends on the track: it is per-frame RMS normalised to the
    # loudest frame and then averaged into half-second buckets, which for most
    # material peaks well below 1.0. A song peaking at 0.37 could never clear a
    # fixed 0.55 drop gate, so every section came back "breakdown".
    high_threshold = float(np.percentile(segment_energies, 65))
    low_threshold = float(np.percentile(segment_energies, 35))
    mid_threshold = float(np.median(segment_energies))
    rise, fall = relative_deltas(segment_energies)

    result = []
    for i in range(len(boundaries) - 1):
        start_ms = int(boundaries[i])
        end_ms = int(boundaries[i + 1])
        value = segment_energies[i]
        prev_value = segment_energies[i - 1] if i > 0 else value
        next_value = segment_energies[i + 1] if i + 1 < len(segment_energies) else value

        if i == 0 and start_ms < duration_ms * 0.18:
            label = "intro"
        elif i == len(segment_energies) - 1:
            label = "outro"
        elif value >= high_threshold:
            label = "drop"
        elif next_value >= high_threshold and next_value > value + rise:
            label = "build"
        elif prev_value >= high_threshold and value <= prev_value - fall:
            label = "breakdown"
        elif value <= low_threshold:
            label = "breakdown"
        elif next_value > value + rise * 0.75:
            label = "build"
        else:
            label = "drop" if value >= mid_threshold else "breakdown"

        result.append({
            "start_ms": start_ms,
            "end_ms": end_ms,
            "type": label,
            "energy": round(float(value), 4),
        })

    return result


def fallback_sections(duration_ms, energy):
    if duration_ms <= 0:
        return []

    boundaries = [
        (0.00, 0.12, "intro"),
        (0.12, 0.18, "build"),
        (0.18, 0.50, "drop"),
        (0.50, 0.62, "breakdown"),
        (0.62, 0.72, "build"),
        (0.72, 0.88, "drop"),
        (0.88, 1.00, "outro"),
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


def clamp_int(value, min_value, max_value):
    return max(min_value, min(int(value), max_value))


if __name__ == "__main__":
    raise SystemExit(main())
