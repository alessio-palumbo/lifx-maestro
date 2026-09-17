package live

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/devices"
)

func TestNormalizePCMLevelUsesRequestedRMS(t *testing.T) {
	samples := normalizePCMLevel([]float32{0.5, -0.5, 0.5, -0.5}, -20)
	var sum float64
	for _, sample := range samples {
		sum += float64(sample * sample)
	}
	got := 10 * math.Log10(sum/float64(len(samples)))
	if math.Abs(got-(-20)) > 0.01 {
		t.Fatalf("normalized RMS = %.2f dB, want -20 dB", got)
	}
}

func TestEvaluatePCMUsesProductionRollingPipeline(t *testing.T) {
	analyzer := &windowAnalyzer{}
	generator, err := NewGenerator(GeneratorConfig{
		Devices: []devices.DeviceInfo{{
			ID:           "test-strip",
			Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, ZoneCount: 8},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	trace, err := EvaluatePCM(context.Background(), "fixture.wav", make([]float32, 10), 10, EvaluationConfig{
		Analyzer: analyzer, Generator: generator,
		WindowDuration: 400 * time.Millisecond,
		HopDuration:    200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Points) != 4 || len(analyzer.windows) != 4 {
		t.Fatalf("evaluation points/windows = %d/%d, want 4/4", len(trace.Points), len(analyzer.windows))
	}
	if !analyzer.closed {
		t.Fatal("evaluation did not close its analyzer")
	}
	if trace.Summary.GeneratedEvents == 0 || len(trace.Events) == 0 {
		t.Fatal("evaluation did not render synthetic device events")
	}
	if trace.Points[0].Intent == "" {
		t.Fatal("evaluation did not retain the generator intent")
	}
}

func TestEvaluatePCMIsDeterministicForTheSameInputs(t *testing.T) {
	run := func() []byte {
		generator, err := NewGenerator(GeneratorConfig{
			Devices: []devices.DeviceInfo{{ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		trace, err := EvaluatePCM(context.Background(), "fixture.wav", make([]float32, 12), 10, EvaluationConfig{
			Analyzer: &windowAnalyzer{}, Generator: generator,
			WindowDuration: 400 * time.Millisecond,
			HopDuration:    200 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(trace)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := run()
	second := run()
	if !bytes.Equal(first, second) {
		t.Fatal("identical evaluation inputs produced different traces")
	}
}

func TestEvaluationComparesLiveAndOfflineBeats(t *testing.T) {
	trace := &EvaluationTrace{
		DurationMS: 2000,
		Points: []EvaluationPoint{
			{AtMS: 500, Beat: true, Active: true, TempoBPM: 120, Dynamics: DynamicsBalanced},
			{AtMS: 1000, Beat: true, Active: true, TempoBPM: 120, Dynamics: DynamicsBalanced},
			{AtMS: 1500, Beat: true, Active: true, TempoBPM: 120, Dynamics: DynamicsBalanced},
		},
	}
	trace.Summary = summarizeEvaluation(trace.Points, nil)
	AddOfflineComparison(trace, &analysis.SongAnalysis{
		DurationMS: 2000,
		BPM:        120,
		Beats:      []int64{510, 990, 1500, 1900},
		Streams:    []analysis.Stream{{ID: "full", Label: "Full", Accents: []int64{500, 1500}}},
		Sections:   []analysis.Section{{StartMS: 0, EndMS: 2000, Type: "section"}},
	}, time.Second)

	if trace.Comparison == nil {
		t.Fatal("offline comparison was not created")
	}
	if trace.Comparison.BeatRecall != 75 || trace.Comparison.BeatPrecision != 100 {
		t.Fatalf("beat recall/precision = %.1f/%.1f, want 75/100", trace.Comparison.BeatRecall, trace.Comparison.BeatPrecision)
	}
	if trace.Comparison.OnsetRecall != 0 || trace.Comparison.OnsetPrecision != 0 {
		t.Fatalf("onset recall/precision = %.1f/%.1f, want 0/0", trace.Comparison.OnsetRecall, trace.Comparison.OnsetPrecision)
	}
	if len(trace.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2", len(trace.Buckets))
	}

	var report bytes.Buffer
	WriteEvaluationReport(&report, trace)
	for _, expected := range []string{"offline tempo", "beat recall/precision", "range", "0-1"} {
		if !strings.Contains(report.String(), expected) {
			t.Fatalf("report %q does not contain %q", report.String(), expected)
		}
	}
}

func TestAddOfflineComparisonStillBucketsWithoutOfflineAnalysis(t *testing.T) {
	trace := &EvaluationTrace{DurationMS: 1000, Points: []EvaluationPoint{{AtMS: 500, Dynamics: DynamicsCalm}}}
	AddOfflineComparison(trace, nil, 500*time.Millisecond)
	if trace.Comparison != nil || len(trace.Buckets) != 2 {
		t.Fatalf("comparison/buckets = %v/%d, want nil/2", trace.Comparison, len(trace.Buckets))
	}
}
