package native_test

import (
	"context"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/internal/devices"
	core "github.com/alessio-palumbo/lifx-maestro/internal/live"
	"github.com/alessio-palumbo/lifx-maestro/live"
	"github.com/alessio-palumbo/lifx-maestro/live/native"
	"github.com/alessio-palumbo/lifx-maestro/live/python"
)

func fixture(rate int) []float32 {
	pcm := make([]float32, 36*rate)
	random := rand.New(rand.NewPCG(1, 2))
	for i := range pcm {
		seconds := float64(i) / float64(rate)
		if seconds < 0.5 || seconds > 34 {
			continue
		}
		x := 0.012 * (1 + 0.3*math.Sin(seconds)) * math.Sin(2*math.Pi*997*seconds)
		if seconds > 10 && seconds < 20 {
			x += 0.002 * (random.Float64()*2 - 1)
		}
		period := 0.5
		if seconds > 6 {
			period = 0.25
		}
		phase := math.Mod(seconds, period)
		if phase < 0.08 {
			x += 0.1 * math.Exp(-phase*30) * (math.Sin(2*math.Pi*110*seconds) + 0.3*math.Sin(2*math.Pi*5017*seconds))
		}
		pcm[i] = float32(x)
	}
	return pcm
}

func evaluation(t *testing.T, a live.Analyzer) core.EvaluationConfig {
	t.Helper()
	g, err := core.NewGenerator(core.GeneratorConfig{Devices: []devices.DeviceInfo{
		{ID: "lamp", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone}},
		{ID: "strip", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMultiZone, ZoneCount: 16}},
		{ID: "matrix", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindMatrix, MatrixWidth: 8, MatrixHeight: 8}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return core.EvaluationConfig{Analyzer: a, Generator: g, Tracker: core.NewStateTracker(core.DefaultTrackerConfig())}
}

func TestNativeIsDeterministicThroughChoreography(t *testing.T) {
	_, _, r, err := core.ComparePCM(context.Background(), "synthetic", fixture(16000), 16000, evaluation(t, native.New()), evaluation(t, native.New()))
	if err != nil {
		t.Fatal(err)
	}
	if r.EventMismatches != 0 || r.DecisionMismatches != 0 || r.OnsetMismatches != 0 || r.CandidateMismatches != 0 {
		t.Fatalf("nondeterministic output: %+v", r)
	}
	for field, value := range r.MaxDifference {
		if value != 0 {
			t.Fatalf("%s difference = %v", field, value)
		}
	}
}

// Optional integration check against the unchanged production Python analyzer.
// Normal Go tests never require Python or invoke an analyzer sidecar.
func TestPythonParity(t *testing.T) {
	path := os.Getenv("MAESTRO_TEST_PYTHON")
	if path == "" {
		t.Skip("set MAESTRO_TEST_PYTHON to enable Python parity tests")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../analyzer/analyze.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, rate := range []int{16000, 44100} {
		t.Run(fmtRate(rate), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			a, err := python.New(ctx, python.Config{PythonPath: path, ScriptPath: script})
			if err != nil {
				t.Fatal(err)
			}
			_, _, r, err := core.ComparePCM(ctx, "synthetic", fixture(rate), rate, evaluation(t, a), evaluation(t, native.New()))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("comparison: %+v", r)
			for field, delta := range r.MaxDifference {
				limit := 0.001
				if field == "tempo_bpm" {
					limit = 0.01
				}
				if delta > limit {
					t.Errorf("%s difference %.6f exceeds %.6f", field, delta, limit)
				}
			}
			if r.OnsetMismatches != 0 || r.BeatMismatches != 0 || r.CandidateMismatches != 0 || r.DecisionMismatches != 0 || r.EventMismatches != 0 {
				t.Errorf("behavior differs: %+v", r)
			}
		})
	}
}
func fmtRate(rate int) string {
	if rate == 16000 {
		return "16k"
	}
	return "44k"
}

func TestNativeAdapterCloseAndCancellation(t *testing.T) {
	a := native.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Analyze(ctx, live.PCMWindow{Samples: []float32{1}, SampleRate: 16000, End: time.Second}); err != context.Canceled {
		t.Fatalf("cancellation = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Analyze(context.Background(), live.PCMWindow{Samples: []float32{1}, SampleRate: 16000, End: time.Second}); err == nil {
		t.Fatal("closed analyzer accepted input")
	}
}
