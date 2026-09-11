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
		_ = json.NewEncoder(os.Stdout).Encode(liveFeatureResponse{
			AtMS:            time.Duration(endNS).Milliseconds(),
			RMSDB:           -24,
			LowDB:           -30,
			MidDB:           -40,
			HighDB:          -50,
			OnsetStrength:   0.8,
			TempoBPM:        120,
			TempoConfidence: 0.9,
			Beat:            true,
		})
	}
}
