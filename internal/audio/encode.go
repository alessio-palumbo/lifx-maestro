package audio

import (
	"fmt"
	"io"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/wav"
)

// WritePCM writes mono 24-bit PCM WAV without normalizing the captured level.
func WritePCM(out io.WriteSeeker, pcm *PCMData) error {
	if pcm == nil || pcm.SampleRate <= 0 || len(pcm.Samples) == 0 {
		return fmt.Errorf("WAV output requires PCM samples and a positive sample rate")
	}
	s := &pcmStreamer{samples: pcm.Samples}
	return wav.Encode(out, s, beep.Format{SampleRate: beep.SampleRate(pcm.SampleRate), NumChannels: 1, Precision: 3})
}

type pcmStreamer struct {
	samples  []float32
	position int
}

func (s *pcmStreamer) Stream(out [][2]float64) (int, bool) {
	n := min(len(out), len(s.samples)-s.position)
	for i := range n {
		v := float64(s.samples[s.position+i])
		out[i] = [2]float64{v, v}
	}
	s.position += n
	return n, n > 0
}
func (*pcmStreamer) Err() error { return nil }
