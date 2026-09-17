package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/timeline"
)

const (
	DefaultEvaluationBucket      = 10 * time.Second
	DefaultEvaluationInputDB     = -42.0
	DefaultEvaluationCalibration = 3 * time.Second
	beatMatchTolerance           = 180 * time.Millisecond
	sectionMatchTolerance        = 3 * time.Second
	evaluationChunkSamples       = 4096
)

type EvaluationConfig struct {
	Analyzer  Analyzer
	Tracker   *StateTracker
	Generator *Generator

	WindowDuration time.Duration
	HopDuration    time.Duration
	InputLevelDB   float64
	Calibration    time.Duration
}

type EvaluationPoint struct {
	AtMS            int64         `json:"at_ms"`
	InputDB         float64       `json:"input_db"`
	NoiseFloorDB    float64       `json:"noise_floor_db"`
	MarginDB        float64       `json:"margin_db"`
	Energy          float64       `json:"energy"`
	Presence        float64       `json:"presence"`
	Low             float64       `json:"low"`
	Mid             float64       `json:"mid"`
	High            float64       `json:"high"`
	Onset           bool          `json:"onset"`
	Beat            bool          `json:"beat"`
	TempoBPM        float64       `json:"tempo_bpm"`
	TempoConfidence float64       `json:"tempo_confidence"`
	Active          bool          `json:"active"`
	Sustained       bool          `json:"sustained"`
	Activity        float64       `json:"activity"`
	Intensity       float64       `json:"intensity"`
	Dynamics        DynamicsLevel `json:"dynamics"`
	Novelty         float64       `json:"novelty"`
	SectionChange   bool          `json:"section_change"`
	Intent          string        `json:"intent,omitempty"`
	Accent          bool          `json:"accent,omitempty"`
	GeneratedEvents int           `json:"generated_events"`
}

type EvaluationTrace struct {
	Source        string             `json:"source"`
	SampleRate    int                `json:"sample_rate"`
	DurationMS    int64              `json:"duration_ms"`
	WindowMS      int64              `json:"window_ms"`
	HopMS         int64              `json:"hop_ms"`
	InputLevelDB  float64            `json:"input_level_db"`
	CalibrationMS int64              `json:"calibration_ms"`
	Points        []EvaluationPoint  `json:"points"`
	Events        []timeline.Event   `json:"events"`
	Summary       EvaluationSummary  `json:"summary"`
	Comparison    *OfflineComparison `json:"offline_comparison,omitempty"`
	Buckets       []EvaluationBucket `json:"buckets,omitempty"`
}

type EvaluationSummary struct {
	Windows            int                `json:"windows"`
	ActivePercent      float64            `json:"active_percent"`
	SustainedPercent   float64            `json:"sustained_percent"`
	StableTempoPercent float64            `json:"stable_tempo_percent"`
	MedianTempoBPM     float64            `json:"median_tempo_bpm"`
	Onsets             int                `json:"onsets"`
	Beats              int                `json:"beats"`
	Sections           int                `json:"sections"`
	GeneratedEvents    int                `json:"generated_events"`
	DynamicsPercent    map[string]float64 `json:"dynamics_percent"`
	IntentCounts       map[string]int     `json:"intent_counts"`
	ActionCounts       map[string]int     `json:"action_counts"`
}

type OfflineComparison struct {
	TempoBPM         float64 `json:"tempo_bpm"`
	Beats            int     `json:"beats"`
	Accents          int     `json:"accents"`
	Sections         int     `json:"sections"`
	BeatRecall       float64 `json:"beat_recall"`
	BeatPrecision    float64 `json:"beat_precision"`
	OnsetRecall      float64 `json:"onset_recall"`
	OnsetPrecision   float64 `json:"onset_precision"`
	SectionRecall    float64 `json:"section_recall"`
	SectionPrecision float64 `json:"section_precision"`
}

type EvaluationBucket struct {
	StartMS         int64         `json:"start_ms"`
	EndMS           int64         `json:"end_ms"`
	MedianTempo     float64       `json:"median_tempo_bpm"`
	AverageEnergy   float64       `json:"average_energy"`
	AveragePresence float64       `json:"average_presence"`
	ActivePercent   float64       `json:"active_percent"`
	Dynamics        DynamicsLevel `json:"dynamics"`
	Onsets          int           `json:"onsets"`
	Beats           int           `json:"beats"`
	OfflineBeats    int           `json:"offline_beats"`
	BeatRecall      float64       `json:"beat_recall"`
	Events          int           `json:"events"`
}

func EvaluatePCM(ctx context.Context, source string, samples []float32, sampleRate int, config EvaluationConfig) (trace *EvaluationTrace, err error) {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil, fmt.Errorf("live evaluation requires PCM samples and a positive sample rate")
	}
	if config.Analyzer == nil {
		return nil, fmt.Errorf("live evaluation analyzer is required")
	}
	if config.Generator == nil {
		return nil, fmt.Errorf("live evaluation generator is required")
	}
	if config.Tracker == nil {
		config.Tracker = NewStateTracker(DefaultTrackerConfig())
	}
	if config.WindowDuration <= 0 {
		config.WindowDuration = DefaultAnalysisWindow
	}
	if config.HopDuration <= 0 {
		config.HopDuration = DefaultAnalysisHop
	}
	if config.HopDuration > config.WindowDuration {
		return nil, fmt.Errorf("live analysis hop must not exceed its window")
	}
	if config.InputLevelDB == 0 {
		config.InputLevelDB = DefaultEvaluationInputDB
	}
	if config.Calibration == 0 {
		config.Calibration = DefaultEvaluationCalibration
	}
	defer func() {
		if closeErr := config.Analyzer.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close live analyzer: %w", closeErr)
		}
	}()

	trace = &EvaluationTrace{
		Source:        source,
		SampleRate:    sampleRate,
		DurationMS:    sampleDuration(sampleRate, int64(len(samples))).Milliseconds(),
		WindowMS:      config.WindowDuration.Milliseconds(),
		HopMS:         config.HopDuration.Milliseconds(),
		InputLevelDB:  config.InputLevelDB,
		CalibrationMS: config.Calibration.Milliseconds(),
	}
	primeEvaluationTracker(config.Tracker, config.Calibration, config.HopDuration)
	samples = normalizePCMLevel(samples, config.InputLevelDB)
	buffer := NewWindowBuffer(sampleRate, config.WindowDuration, config.HopDuration)
	for start := 0; start < len(samples); start += evaluationChunkSamples {
		end := start + evaluationChunkSamples
		if end > len(samples) {
			end = len(samples)
		}
		chunk := PCMChunk{Samples: samples[start:end], SampleRate: sampleRate, CapturedAt: sampleDuration(sampleRate, int64(end))}
		for _, window := range buffer.Push(chunk) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			features, analyzeErr := config.Analyzer.Analyze(ctx, window)
			if analyzeErr != nil {
				return nil, fmt.Errorf("analyze live evaluation at %s: %w", window.End, analyzeErr)
			}
			state := config.Tracker.Update(features)
			decision := config.Generator.GenerateDecision(state)
			trace.Events = append(trace.Events, decision.Events...)
			trace.Points = append(trace.Points, evaluationPoint(state, decision))
		}
	}
	trace.Summary = summarizeEvaluation(trace.Points, trace.Events)
	return trace, nil
}

func AddOfflineComparison(trace *EvaluationTrace, offline *analysis.SongAnalysis, bucketDuration time.Duration) {
	if trace == nil {
		return
	}
	if bucketDuration <= 0 {
		bucketDuration = DefaultEvaluationBucket
	}
	liveBeats := pointTimes(trace.Points, func(point EvaluationPoint) bool { return point.Active && point.Beat })
	liveOnsets := pointTimes(trace.Points, func(point EvaluationPoint) bool { return point.Active && point.Onset })
	liveSections := pointTimes(trace.Points, func(point EvaluationPoint) bool { return point.SectionChange })
	var offlineBeats []time.Duration
	if offline != nil {
		offlineBeats = durationsFromMS(offline.Beats)
		var offlineAccents []time.Duration
		for _, stream := range offline.Streams {
			if stream.ID == "full" {
				offlineAccents = durationsFromMS(stream.Accents)
				break
			}
		}
		offlineSections := make([]time.Duration, 0, len(offline.Sections))
		for _, section := range offline.Sections {
			if section.StartMS > 0 {
				offlineSections = append(offlineSections, time.Duration(section.StartMS)*time.Millisecond)
			}
		}
		trace.Comparison = &OfflineComparison{
			TempoBPM:         offline.BPM,
			Beats:            len(offlineBeats),
			Accents:          len(offlineAccents),
			Sections:         len(offlineSections),
			BeatRecall:       matchRatio(offlineBeats, liveBeats, beatMatchTolerance),
			BeatPrecision:    matchRatio(liveBeats, offlineBeats, beatMatchTolerance),
			OnsetRecall:      matchRatio(offlineAccents, liveOnsets, beatMatchTolerance),
			OnsetPrecision:   matchRatio(liveOnsets, offlineAccents, beatMatchTolerance),
			SectionRecall:    matchRatio(offlineSections, liveSections, sectionMatchTolerance),
			SectionPrecision: matchRatio(liveSections, offlineSections, sectionMatchTolerance),
		}
	}

	for start := time.Duration(0); start < time.Duration(trace.DurationMS)*time.Millisecond; start += bucketDuration {
		end := start + bucketDuration
		if end > time.Duration(trace.DurationMS)*time.Millisecond {
			end = time.Duration(trace.DurationMS) * time.Millisecond
		}
		trace.Buckets = append(trace.Buckets, summarizeBucket(trace, offlineBeats, start, end))
	}
}

func SaveEvaluationTrace(path string, trace *EvaluationTrace) error {
	if trace == nil {
		return fmt.Errorf("live evaluation trace is required")
	}
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("encode live evaluation trace: %w", err)
	}
	data = append(data, '\n')
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create live evaluation output directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write live evaluation trace: %w", err)
	}
	return nil
}

func WriteEvaluationReport(out io.Writer, trace *EvaluationTrace) {
	if trace == nil {
		return
	}
	summary := trace.Summary
	fmt.Fprintf(out, "file                 %s\n", trace.Source)
	fmt.Fprintf(out, "duration             %.1fs\n", float64(trace.DurationMS)/1000)
	fmt.Fprintf(out, "simulated input      %.1f dB RMS after %.1fs calibration\n", trace.InputLevelDB, float64(trace.CalibrationMS)/1000)
	fmt.Fprintf(out, "live stable tempo    %.1f BPM\n", summary.MedianTempoBPM)
	fmt.Fprintf(out, "stable-clock coverage %.1f%%\n", summary.StableTempoPercent)
	fmt.Fprintf(out, "active / ambient     %.1f%% / %.1f%%\n", summary.ActivePercent, summary.SustainedPercent)
	fmt.Fprintf(out, "onsets / beats       %d / %d\n", summary.Onsets, summary.Beats)
	fmt.Fprintf(out, "sections / events    %d / %d\n", summary.Sections, summary.GeneratedEvents)
	fmt.Fprintf(out, "dynamics             calm %.1f%%  balanced %.1f%%  energetic %.1f%%\n",
		summary.DynamicsPercent[string(DynamicsCalm)], summary.DynamicsPercent[string(DynamicsBalanced)], summary.DynamicsPercent[string(DynamicsEnergetic)])
	fmt.Fprintf(out, "intents              %s\n", formatIntCounts(summary.IntentCounts))
	if comparison := trace.Comparison; comparison != nil {
		fmt.Fprintf(out, "offline tempo        %.1f BPM\n", comparison.TempoBPM)
		fmt.Fprintf(out, "beat recall/precision %.1f%% / %.1f%%\n", comparison.BeatRecall, comparison.BeatPrecision)
		fmt.Fprintf(out, "onset recall/precision %.1f%% / %.1f%%\n", comparison.OnsetRecall, comparison.OnsetPrecision)
		fmt.Fprintf(out, "section recall/precision %.1f%% / %.1f%%\n", comparison.SectionRecall, comparison.SectionPrecision)
	}
	if len(trace.Buckets) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "range       tempo  active energy presence dynamics   onsets beats/offline recall events")
	for _, bucket := range trace.Buckets {
		fmt.Fprintf(out, "%5.0f-%-5.0f %6.1f %6.1f%% %6.2f %8.2f %-10s %6d %5d/%-7d %5.1f%% %6d\n",
			float64(bucket.StartMS)/1000, float64(bucket.EndMS)/1000, bucket.MedianTempo, bucket.ActivePercent,
			bucket.AverageEnergy, bucket.AveragePresence, bucket.Dynamics, bucket.Onsets, bucket.Beats, bucket.OfflineBeats, bucket.BeatRecall, bucket.Events)
	}
}

func primeEvaluationTracker(tracker *StateTracker, duration, hop time.Duration) {
	if tracker == nil || duration <= 0 || hop <= 0 {
		return
	}
	level := tracker.config.InitialNoiseFloorDB
	for at := -duration; at < 0; at += hop {
		tracker.Update(Features{At: at, RMSDB: level, LowDB: level, MidDB: level, HighDB: level})
	}
}

func normalizePCMLevel(samples []float32, targetDB float64) []float32 {
	var sum float64
	for _, sample := range samples {
		value := float64(sample)
		sum += value * value
	}
	if len(samples) == 0 || sum == 0 {
		return append([]float32(nil), samples...)
	}
	currentDB := 20 * math.Log10(math.Sqrt(sum/float64(len(samples))))
	gain := math.Pow(10, (targetDB-currentDB)/20)
	result := make([]float32, len(samples))
	for i, sample := range samples {
		result[i] = float32(clamp(float64(sample)*gain, -1, 1))
	}
	return result
}

func evaluationPoint(state State, decision GenerationDecision) EvaluationPoint {
	return EvaluationPoint{
		AtMS: state.At.Milliseconds(), InputDB: state.InputDB, NoiseFloorDB: state.NoiseFloorDB, MarginDB: state.MarginDB,
		Energy: state.Energy, Presence: state.Presence, Low: state.Low, Mid: state.Mid, High: state.High,
		Onset: state.Onset, Beat: state.Beat, TempoBPM: state.TempoBPM, TempoConfidence: state.TempoConfidence,
		Active: state.Active, Sustained: state.Sustained, Activity: state.Activity, Intensity: state.Intensity,
		Dynamics: state.Dynamics, Novelty: state.Novelty, SectionChange: state.SectionChange,
		Intent: string(decision.Intent), Accent: decision.Accent, GeneratedEvents: len(decision.Events),
	}
}

func summarizeEvaluation(points []EvaluationPoint, events []timeline.Event) EvaluationSummary {
	summary := EvaluationSummary{Windows: len(points), DynamicsPercent: map[string]float64{}, IntentCounts: map[string]int{}, ActionCounts: map[string]int{}}
	var tempos []float64
	for _, point := range points {
		if point.Active {
			summary.ActivePercent++
		}
		if point.Sustained {
			summary.SustainedPercent++
		}
		if point.TempoBPM > 0 {
			summary.StableTempoPercent++
			tempos = append(tempos, point.TempoBPM)
		}
		if point.Active && point.Onset {
			summary.Onsets++
		}
		if point.Active && point.Beat {
			summary.Beats++
		}
		if point.SectionChange {
			summary.Sections++
		}
		summary.DynamicsPercent[string(point.Dynamics)]++
		if point.Intent != "" {
			summary.IntentCounts[point.Intent]++
		}
	}
	for _, event := range events {
		summary.ActionCounts[event.Action]++
	}
	summary.GeneratedEvents = len(events)
	if len(points) > 0 {
		denominator := float64(len(points))
		summary.ActivePercent = summary.ActivePercent / denominator * 100
		summary.SustainedPercent = summary.SustainedPercent / denominator * 100
		summary.StableTempoPercent = summary.StableTempoPercent / denominator * 100
		for key, value := range summary.DynamicsPercent {
			summary.DynamicsPercent[key] = value / denominator * 100
		}
	}
	summary.MedianTempoBPM = median(tempos)
	return summary
}

func summarizeBucket(trace *EvaluationTrace, offlineBeats []time.Duration, start, end time.Duration) EvaluationBucket {
	bucket := EvaluationBucket{StartMS: start.Milliseconds(), EndMS: end.Milliseconds()}
	var points []EvaluationPoint
	var tempos []float64
	dynamics := map[DynamicsLevel]int{}
	for _, point := range trace.Points {
		at := time.Duration(point.AtMS) * time.Millisecond
		if at < start || at >= end {
			continue
		}
		points = append(points, point)
		bucket.AverageEnergy += point.Energy
		bucket.AveragePresence += point.Presence
		if point.Active {
			bucket.ActivePercent++
		}
		if point.TempoBPM > 0 {
			tempos = append(tempos, point.TempoBPM)
		}
		if point.Active && point.Onset {
			bucket.Onsets++
		}
		if point.Active && point.Beat {
			bucket.Beats++
		}
		bucket.Events += point.GeneratedEvents
		dynamics[point.Dynamics]++
	}
	if len(points) > 0 {
		denominator := float64(len(points))
		bucket.AverageEnergy /= denominator
		bucket.AveragePresence /= denominator
		bucket.ActivePercent = bucket.ActivePercent / denominator * 100
	}
	bucket.MedianTempo = median(tempos)
	bucket.Dynamics = dominantDynamics(dynamics)
	liveBeats := pointTimes(points, func(point EvaluationPoint) bool { return point.Active && point.Beat })
	boundedOffline := boundedTimes(offlineBeats, start, end)
	bucket.OfflineBeats = len(boundedOffline)
	bucket.BeatRecall = matchRatio(boundedOffline, liveBeats, beatMatchTolerance)
	return bucket
}

func pointTimes(points []EvaluationPoint, predicate func(EvaluationPoint) bool) []time.Duration {
	var values []time.Duration
	for _, point := range points {
		if predicate(point) {
			values = append(values, time.Duration(point.AtMS)*time.Millisecond)
		}
	}
	return values
}

func durationsFromMS(values []int64) []time.Duration {
	result := make([]time.Duration, len(values))
	for i, value := range values {
		result[i] = time.Duration(value) * time.Millisecond
	}
	return result
}

func boundedTimes(values []time.Duration, start, end time.Duration) []time.Duration {
	var result []time.Duration
	for _, value := range values {
		if value >= start && value < end {
			result = append(result, value)
		}
	}
	return result
}

func matchRatio(reference, candidates []time.Duration, tolerance time.Duration) float64 {
	if len(reference) == 0 {
		return 0
	}
	matches := 0
	for _, value := range reference {
		for _, candidate := range candidates {
			if absDuration(value-candidate) <= tolerance {
				matches++
				break
			}
		}
	}
	return float64(matches) / float64(len(reference)) * 100
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return (values[middle-1] + values[middle]) / 2
	}
	return values[middle]
}

func dominantDynamics(counts map[DynamicsLevel]int) DynamicsLevel {
	selected := DynamicsCalm
	best := -1
	for _, candidate := range []DynamicsLevel{DynamicsCalm, DynamicsBalanced, DynamicsEnergetic} {
		if counts[candidate] > best {
			selected, best = candidate, counts[candidate]
		}
	}
	return selected
}

func formatIntCounts(values map[string]int) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := ""
	for _, key := range keys {
		if result != "" {
			result += ", "
		}
		result += fmt.Sprintf("%s=%d", key, values[key])
	}
	return result
}
