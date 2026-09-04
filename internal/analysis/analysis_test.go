package analysis

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAnalyzerRunsSubprocessAndParsesJSON(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "analyze.py")
	if err := os.WriteFile(script, []byte(`import json
import sys
print(json.dumps({"duration_ms": 1000, "bpm": 120, "beats": [0, 500], "energy": [{"time_ms": 0, "value": 0.5}], "streams": [{"id": "low", "label": "Low / bass", "energy": [{"time_ms": 0, "value": 0.4}], "accents": [125]}]}))
`), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer := Analyzer{
		PythonPath: pythonPath(),
		ScriptPath: script,
	}

	got, err := analyzer.Analyze(context.Background(), "song.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if got.DurationMS != 1000 || got.BPM != 120 || len(got.Beats) != 2 {
		t.Fatalf("unexpected analysis: %+v", got)
	}
	if len(got.Streams) != 1 || got.Streams[0].ID != "low" || len(got.Streams[0].Accents) != 1 {
		t.Fatalf("unexpected streams: %+v", got.Streams)
	}
}

func TestSongAnalysisValidatesStreams(t *testing.T) {
	err := (SongAnalysis{
		DurationMS: 1000,
		Streams: []Stream{
			{ID: "low", Label: "Low / bass", Energy: []EnergyPoint{{TimeMS: 0, Value: 1.2}}},
		},
	}).Validate()
	if err == nil {
		t.Fatal("expected invalid stream energy error")
	}
}

func pythonPath() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}
