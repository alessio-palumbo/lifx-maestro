package live

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"lifx-maestro/internal/analysis"
)

type PythonAnalyzer struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	decoder *json.Decoder
	stderr  bytes.Buffer

	mu     sync.Mutex
	closed bool
}

func NewPythonAnalyzer(ctx context.Context, analyzer analysis.Analyzer) (*PythonAnalyzer, error) {
	var cmd *exec.Cmd
	if analyzer.BinaryPath != "" {
		cmd = exec.CommandContext(ctx, analyzer.BinaryPath, "--live")
	} else {
		if analyzer.PythonPath == "" || analyzer.ScriptPath == "" {
			return nil, fmt.Errorf("live analyzer requires a binary or Python interpreter and script")
		}
		cmd = exec.CommandContext(ctx, analyzer.PythonPath, analyzer.ScriptPath, "--live")
	}
	cmd.Dir = os.TempDir()
	return startPythonAnalyzer(cmd)
}

func startPythonAnalyzer(cmd *exec.Cmd) (*PythonAnalyzer, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open live analyzer stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open live analyzer stdout: %w", err)
	}
	analyzer := &PythonAnalyzer{cmd: cmd, stdin: stdin, decoder: json.NewDecoder(stdout)}
	cmd.Stderr = &analyzer.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start live analyzer: %w", err)
	}
	return analyzer, nil
}

func (a *PythonAnalyzer) Analyze(ctx context.Context, window PCMWindow) (Features, error) {
	if err := ctx.Err(); err != nil {
		return Features{}, err
	}
	if window.SampleRate <= 0 || len(window.Samples) == 0 {
		return Features{}, fmt.Errorf("live analysis window requires PCM samples and a positive sample rate")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return Features{}, errors.New("live analyzer is closed")
	}

	var request bytes.Buffer
	if err := binary.Write(&request, binary.LittleEndian, uint32(window.SampleRate)); err != nil {
		return Features{}, err
	}
	if err := binary.Write(&request, binary.LittleEndian, uint32(len(window.Samples))); err != nil {
		return Features{}, err
	}
	if err := binary.Write(&request, binary.LittleEndian, window.End.Nanoseconds()); err != nil {
		return Features{}, err
	}
	if err := binary.Write(&request, binary.LittleEndian, window.Samples); err != nil {
		return Features{}, err
	}
	if _, err := a.stdin.Write(request.Bytes()); err != nil {
		return Features{}, a.processError("write PCM window", err)
	}

	var response liveFeatureResponse
	if err := a.decoder.Decode(&response); err != nil {
		return Features{}, a.processError("read feature response", err)
	}
	return response.features(), nil
}

func (a *PythonAnalyzer) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	_ = a.stdin.Close()
	if err := a.cmd.Wait(); err != nil && a.cmd.ProcessState != nil && !a.cmd.ProcessState.Success() {
		return a.processError("stop", err)
	}
	return nil
}

func (a *PythonAnalyzer) processError(operation string, err error) error {
	detail := a.stderr.String()
	if detail == "" {
		return fmt.Errorf("live analyzer %s: %w", operation, err)
	}
	return fmt.Errorf("live analyzer %s: %w: %s", operation, err, detail)
}

type liveFeatureResponse struct {
	AtMS            int64   `json:"at_ms"`
	RMSDB           float64 `json:"rms_db"`
	LowDB           float64 `json:"low_db"`
	MidDB           float64 `json:"mid_db"`
	HighDB          float64 `json:"high_db"`
	OnsetStrength   float64 `json:"onset_strength"`
	TempoBPM        float64 `json:"tempo_bpm"`
	TempoConfidence float64 `json:"tempo_confidence"`
	Beat            bool    `json:"beat"`
}

func (r liveFeatureResponse) features() Features {
	return Features{
		At:              time.Duration(r.AtMS) * time.Millisecond,
		RMSDB:           r.RMSDB,
		LowDB:           r.LowDB,
		MidDB:           r.MidDB,
		HighDB:          r.HighDB,
		OnsetStrength:   r.OnsetStrength,
		TempoBPM:        r.TempoBPM,
		TempoConfidence: r.TempoConfidence,
		Beat:            r.Beat,
	}
}
