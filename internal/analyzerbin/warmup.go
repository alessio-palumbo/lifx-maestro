package analyzerbin

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// warmupTimeout is generous: the first run compiles the analyzer's hot paths
// while the OS verifies every library in the bundle.
const warmupTimeout = 5 * time.Minute

// NeedsWarmup reports whether the bundled analyzer still owes its first run.
//
// Extracting the bundle is the cheap part of first-run cost. The expensive part
// is the first execution, where the OS verifies every shipped library and the
// analyzer's JIT compiles its hot paths. Those caches persist on disk, so this is
// keyed to the install rather than the process, and an interrupted warmup is
// retried on the next launch.
//
// It returns false for development builds, which have nothing to warm.
func NeedsWarmup() bool {
	if !Bundled() {
		return false
	}
	baseDir, err := installDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(warmFilePath(baseDir))
	return err != nil || strings.TrimSpace(string(data)) != Version
}

// Warm runs one throwaway analysis so the user's first real analysis does not
// pay for it, recording success so later launches skip the work.
func Warm(ctx context.Context, exePath string) error {
	audioPath, err := writeWarmupAudio()
	if err != nil {
		return err
	}
	defer os.Remove(audioPath)

	ctx, cancel := context.WithTimeout(ctx, warmupTimeout)
	defer cancel()

	// Output is discarded; running the analysis at all is the point.
	if err := exec.CommandContext(ctx, exePath, audioPath).Run(); err != nil {
		return fmt.Errorf("warm analyzer: %w", err)
	}
	return MarkWarm()
}

// MarkWarm records that the analyzer has completed a run.
func MarkWarm() error {
	if !Bundled() {
		return nil
	}
	baseDir, err := installDir()
	if err != nil {
		return err
	}
	return os.WriteFile(warmFilePath(baseDir), []byte(Version), 0o644)
}

func warmFilePath(baseDir string) string {
	return filepath.Join(baseDir, ".analyzer-warm")
}

const (
	// 44.1kHz matches the rate of real songs. Warming at 22.05kHz measurably
	// helped less and took twice as long, since librosa then resamples.
	warmupSampleRate = 44100
	// analyze.py bins section features per second and gives up below eight bins,
	// so a short clip would leave the section-detection path cold.
	warmupSeconds = 10
	warmupToneHz  = 220
)

// writeWarmupAudio synthesises a tone rather than silence: beat tracking and
// feature extraction are where the analyzer's JIT compiles, and silence lets
// those paths short-circuit.
func writeWarmupAudio() (string, error) {
	file, err := os.CreateTemp("", "lifx-maestro-warmup-*.wav")
	if err != nil {
		return "", fmt.Errorf("create warmup audio: %w", err)
	}
	defer file.Close()

	samples := warmupSampleRate * warmupSeconds
	if err := writeWAVHeader(file, samples); err != nil {
		os.Remove(file.Name())
		return "", err
	}

	pcm := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		phase := 2 * math.Pi * warmupToneHz * float64(i) / warmupSampleRate
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(math.Sin(phase)*0.2*math.MaxInt16)))
	}
	if _, err := file.Write(pcm); err != nil {
		os.Remove(file.Name())
		return "", fmt.Errorf("write warmup audio: %w", err)
	}
	return file.Name(), nil
}

// writeWAVHeader writes a 44-byte canonical RIFF header for 16-bit mono PCM.
func writeWAVHeader(file *os.File, samples int) error {
	dataSize := uint32(samples * 2)
	header := make([]byte, 0, 44)
	header = append(header, "RIFF"...)
	header = binary.LittleEndian.AppendUint32(header, 36+dataSize)
	header = append(header, "WAVEfmt "...)
	header = binary.LittleEndian.AppendUint32(header, 16) // PCM chunk size
	header = binary.LittleEndian.AppendUint16(header, 1)  // PCM format
	header = binary.LittleEndian.AppendUint16(header, 1)  // mono
	header = binary.LittleEndian.AppendUint32(header, warmupSampleRate)
	header = binary.LittleEndian.AppendUint32(header, warmupSampleRate*2) // byte rate
	header = binary.LittleEndian.AppendUint16(header, 2)                  // block align
	header = binary.LittleEndian.AppendUint16(header, 16)                 // bits per sample
	header = append(header, "data"...)
	header = binary.LittleEndian.AppendUint32(header, dataSize)

	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("write warmup audio header: %w", err)
	}
	return nil
}
