package live

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPythonAnalyzerUsesPersistentBinaryProtocol(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestPythonAnalyzerHelper")
	cmd.Env = append(os.Environ(), "MAESTRO_LIVE_ANALYZER_HELPER=1")
	analyzer, err := startPythonAnalyzer(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := analyzer.Close(); err != nil {
			t.Errorf("close analyzer: %v", err)
		}
	})

	for index := range 2 {
		end := time.Duration(index+1) * 100 * time.Millisecond
		features, err := analyzer.Analyze(context.Background(), PCMWindow{
			Samples:    []float32{0.1, -0.2, 0.3, -0.4},
			SampleRate: 16_000,
			End:        end,
		})
		if err != nil {
			t.Fatal(err)
		}
		if features.At != end {
			t.Fatalf("response %d timestamp = %v, want %v", index, features.At, end)
		}
		if features.LowDB != -30 || !features.Beat {
			t.Fatalf("response %d = %+v", index, features)
		}
		if !features.HasRecentRMS || features.RecentRMSDB != -22 {
			t.Fatalf("response %d recent RMS = %.1f (available=%t)", index, features.RecentRMSDB, features.HasRecentRMS)
		}
		if !features.HasRecentBands || features.RecentMidDB != -32 {
			t.Fatalf("response %d recent bands = %.1f/%.1f/%.1f (available=%t)", index, features.RecentLowDB, features.RecentMidDB, features.RecentHighDB, features.HasRecentBands)
		}
		if len(features.TempoCandidates) != 1 || features.TempoCandidates[0].BPM != 80 {
			t.Fatalf("response %d tempo candidates = %+v", index, features.TempoCandidates)
		}
	}
}

func TestPythonAnalyzerHelper(t *testing.T) {
	if os.Getenv("MAESTRO_LIVE_ANALYZER_HELPER") != "1" {
		return
	}
	for {
		var sampleRate uint32
		if err := binary.Read(os.Stdin, binary.LittleEndian, &sampleRate); err != nil {
			if err == io.EOF {
				return
			}
			os.Exit(2)
		}
		var sampleCount uint32
		var endNS int64
		if binary.Read(os.Stdin, binary.LittleEndian, &sampleCount) != nil ||
			binary.Read(os.Stdin, binary.LittleEndian, &endNS) != nil {
			os.Exit(2)
		}
		samples := make([]float32, sampleCount)
		if binary.Read(os.Stdin, binary.LittleEndian, samples) != nil {
			os.Exit(2)
		}
		if sampleRate == 0 || len(samples) == 0 {
			os.Exit(2)
		}
		recentRMSDB := -22.0
		recentLowDB, recentMidDB, recentHighDB := -30.0, -32.0, -40.0
		_ = json.NewEncoder(os.Stdout).Encode(liveFeatureResponse{
			AtMS:            time.Duration(endNS).Milliseconds(),
			RMSDB:           -24,
			RecentRMSDB:     &recentRMSDB,
			RecentLowDB:     &recentLowDB,
			RecentMidDB:     &recentMidDB,
			RecentHighDB:    &recentHighDB,
			LowDB:           -30,
			MidDB:           -40,
			HighDB:          -50,
			OnsetStrength:   0.8,
			Onset:           true,
			TempoBPM:        120,
			TempoConfidence: 0.9,
			TempoCandidates: []struct {
				BPM        float64 `json:"bpm"`
				Confidence float64 `json:"confidence"`
				Strength   float64 `json:"strength"`
			}{{BPM: 80, Confidence: 0.8, Strength: 0.7}},
			Beat: true,
		})
	}
}
