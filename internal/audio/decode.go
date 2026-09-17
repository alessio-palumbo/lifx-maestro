package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/wav"
)

type PCMData struct {
	Samples    []float32
	SampleRate int
}

// DecodePCM decodes an audio file to mono PCM without opening an output device.
func DecodePCM(path string) (*PCMData, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}

	var (
		streamer beep.StreamSeekCloser
		format   beep.Format
	)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		streamer, format, err = mp3.Decode(file)
	case ".wav":
		streamer, format, err = wav.Decode(file)
	default:
		_ = file.Close()
		return nil, fmt.Errorf("live evaluation supports MP3 and WAV files")
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("decode audio file: %w", err)
	}
	defer streamer.Close()

	capacity := streamer.Len()
	if capacity < 0 {
		capacity = 0
	}
	samples := make([]float32, 0, capacity)
	frames := make([][2]float64, 4096)
	for {
		n, ok := streamer.Stream(frames)
		for _, frame := range frames[:n] {
			samples = append(samples, float32((frame[0]+frame[1])/2))
		}
		if !ok {
			break
		}
	}
	if err := streamer.Err(); err != nil {
		return nil, fmt.Errorf("decode audio samples: %w", err)
	}
	if len(samples) == 0 || format.SampleRate <= 0 {
		return nil, fmt.Errorf("audio file contains no decodable samples")
	}
	return &PCMData{Samples: samples, SampleRate: int(format.SampleRate)}, nil
}
