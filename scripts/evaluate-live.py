#!/usr/bin/env python3
"""Compare causal Maestro Live analysis with full-track Librosa analysis."""

import argparse
import importlib.util
from pathlib import Path

import librosa
import numpy as np


WINDOW_SECONDS = 0.5
HOP_SECONDS = 0.1
MATCH_TOLERANCE_SECONDS = 0.18
REPORT_BUCKET_SECONDS = 10.0
RELIABLE_CONFIDENCE = 0.45


def load_live_analyzer():
    script = Path(__file__).resolve().parents[1] / "analyzer" / "analyze.py"
    spec = importlib.util.spec_from_file_location("maestro_analyzer", script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.LiveWindowAnalyzer


def nearest_match_count(reference, candidates, tolerance):
    if len(reference) == 0 or len(candidates) == 0:
        return 0
    return sum(float(np.min(np.abs(candidates - value))) <= tolerance for value in reference)


def longest_gap(values, start, end):
    bounded = values[(values >= start) & (values < end)]
    points = np.concatenate(([start], bounded, [end]))
    return float(np.max(np.diff(points))) if len(points) > 1 else end - start


def stabilize_tempo(candidate):
    while candidate > 140:
        candidate /= 2
    while candidate < 70:
        candidate *= 2
    return candidate


def simulate_tracker_clock(rows):
    """Mirror the Go stable-tempo and predicted-beat policy for diagnostics."""
    tempo = 0.0
    confidence = 0.0
    next_beat = None
    stable_tempos = []
    stable_confidence = []
    clock_beats = []

    for row in rows:
        at = row["at_ms"] / 1000.0
        raw_confidence = row["tempo_confidence"]
        if raw_confidence >= RELIABLE_CONFIDENCE and 40 <= row["tempo_bpm"] <= 240:
            candidate = stabilize_tempo(row["tempo_bpm"])
            tempo = candidate if tempo <= 0 else tempo * 0.88 + candidate * 0.12
            confidence = raw_confidence if confidence <= 0 else confidence * 0.8 + raw_confidence * 0.2
        else:
            confidence *= 0.985

        reported = tempo
        stable_tempos.append(reported)
        stable_confidence.append(confidence)
        if reported <= 0:
            next_beat = None
            continue

        period = 60.0 / reported
        observed = bool(row["beat"] and raw_confidence >= RELIABLE_CONFIDENCE)
        if next_beat is None:
            next_beat = at + period
            if observed:
                clock_beats.append(at)
            continue
        if observed and abs(at - next_beat) <= period / 4:
            clock_beats.append(at)
            next_beat = at + period
            continue
        if at >= next_beat:
            clock_beats.append(at)
            while next_beat <= at:
                next_beat += period

    return (
        np.asarray(stable_tempos),
        np.asarray(stable_confidence),
        np.asarray(clock_beats),
    )


def evaluate(path):
    samples, sample_rate = librosa.load(path, sr=None, mono=True)
    duration = librosa.get_duration(y=samples, sr=sample_rate)

    onset_envelope = librosa.onset.onset_strength(y=samples, sr=sample_rate)
    offline_tempo, offline_frames = librosa.beat.beat_track(
        y=samples,
        sr=sample_rate,
        onset_envelope=onset_envelope,
    )
    offline_tempo = float(np.asarray(offline_tempo).reshape(-1)[0]) if np.size(offline_tempo) else 0.0
    offline_beats = librosa.frames_to_time(offline_frames, sr=sample_rate)

    analyzer = load_live_analyzer()()
    window_size = max(1, int(round(sample_rate * WINDOW_SECONDS)))
    hop_size = max(1, int(round(sample_rate * HOP_SECONDS)))
    live_rows = []
    for end in range(window_size, len(samples) + 1, hop_size):
        result = analyzer.analyze(samples[end - window_size:end], sample_rate, end / sample_rate)
        live_rows.append(result)

    live_times = np.asarray([row["at_ms"] / 1000.0 for row in live_rows])
    live_anchors = live_times[np.asarray([row["beat"] for row in live_rows], dtype=bool)]
    stable_tempos, _, clock_beats = simulate_tracker_clock(live_rows)

    stable = stable_tempos > 0
    median_tempo = float(np.median(stable_tempos[stable])) if np.any(stable) else 0.0
    anchor_recall = nearest_match_count(offline_beats, live_anchors, MATCH_TOLERANCE_SECONDS) / max(len(offline_beats), 1)
    anchor_precision = nearest_match_count(live_anchors, offline_beats, MATCH_TOLERANCE_SECONDS) / max(len(live_anchors), 1)
    clock_recall = nearest_match_count(offline_beats, clock_beats, MATCH_TOLERANCE_SECONDS) / max(len(offline_beats), 1)
    clock_precision = nearest_match_count(clock_beats, offline_beats, MATCH_TOLERANCE_SECONDS) / max(len(clock_beats), 1)

    print(f"file                 {path}")
    print(f"duration             {duration:.1f}s")
    print(f"offline tempo         {offline_tempo:.1f} BPM")
    print(f"live stable tempo     {median_tempo:.1f} BPM")
    print(f"stable-clock coverage {100.0 * np.mean(stable):.1f}%")
    print(f"beat-anchor recall    {100.0 * anchor_recall:.1f}%")
    print(f"beat-anchor precision {100.0 * anchor_precision:.1f}%")
    print(f"clock-beat recall     {100.0 * clock_recall:.1f}%")
    print(f"clock-beat precision  {100.0 * clock_precision:.1f}%")
    print()
    print("range       tempo   clock on  anchors  beats  offline  recall  longest gap")

    bucket_start = 0.0
    while bucket_start < duration:
        bucket_end = min(duration, bucket_start + REPORT_BUCKET_SECONDS)
        live_mask = (live_times >= bucket_start) & (live_times < bucket_end)
        stable_mask = live_mask & stable
        bucket_tempos = stable_tempos[stable_mask]
        bucket_tempo = float(np.median(bucket_tempos)) if len(bucket_tempos) else 0.0
        anchors = live_anchors[(live_anchors >= bucket_start) & (live_anchors < bucket_end)]
        clock = clock_beats[(clock_beats >= bucket_start) & (clock_beats < bucket_end)]
        beats = offline_beats[(offline_beats >= bucket_start) & (offline_beats < bucket_end)]
        bucket_recall = nearest_match_count(beats, clock, MATCH_TOLERANCE_SECONDS) / max(len(beats), 1)
        coverage = float(np.mean(stable[live_mask])) if np.any(live_mask) else 0.0
        gap = longest_gap(clock_beats, bucket_start, bucket_end)
        print(
            f"{bucket_start:5.0f}-{bucket_end:<5.0f} "
            f"{bucket_tempo:7.1f} {coverage * 100:9.1f}% "
            f"{len(anchors):8d} {len(clock):6d} {len(beats):8d} "
            f"{bucket_recall * 100:6.1f}% {gap:10.2f}s"
        )
        bucket_start = bucket_end


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("audio_file", help="MP3/WAV file to evaluate")
    args = parser.parse_args()
    evaluate(args.audio_file)


if __name__ == "__main__":
    main()
