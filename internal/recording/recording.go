// Package recording captures explicitly requested, bounded microphone fixtures.
package recording

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alessio-palumbo/lifx-maestro/internal/audio"
	"github.com/alessio-palumbo/lifx-maestro/live"
)

const MaxDuration = 10 * time.Minute

// Capture records at most duration of mono PCM. Cancellation returns the samples
// recorded so far with the context error. The source must honor cancellation;
// Capture joins it before returning. No analyzer or device controller is used.
func Capture(ctx context.Context, source live.AudioSource, rate int, duration time.Duration) (*audio.PCMData, error) {
	if source == nil || rate <= 0 || rate > 192000 || duration <= 0 || duration > MaxDuration {
		return nil, fmt.Errorf("recording requires a source, a sample rate up to 192000 Hz, and a duration up to %s", MaxDuration)
	}
	limit := int(int64(rate) * int64(duration) / int64(time.Second))
	if limit < 1 {
		return nil, fmt.Errorf("recording duration is shorter than one sample")
	}
	// Duration is measured in received samples, so opening the microphone does
	// not shorten the clip. A watchdog also bounds a source that sends no audio.
	runCtx, cancel := context.WithTimeout(ctx, duration+10*time.Second)
	defer cancel()
	chunks := make(chan live.PCMChunk, 16)
	done := make(chan error, 1)
	go func() { defer close(chunks); done <- source.Run(runCtx, chunks) }()
	pcm := &audio.PCMData{SampleRate: rate, Samples: make([]float32, 0, limit)}
	var captureErr error
loop:
	for len(pcm.Samples) < limit {
		select {
		case <-runCtx.Done():
			captureErr = runCtx.Err()
			break loop
		case chunk, ok := <-chunks:
			if !ok {
				break loop
			}
			if chunk.SampleRate != rate {
				captureErr = fmt.Errorf("recording sample rate changed from %d to %d", rate, chunk.SampleRate)
				break loop
			}
			count := min(len(chunk.Samples), limit-len(pcm.Samples))
			pcm.Samples = append(pcm.Samples, chunk.Samples[:count]...)
		}
	}
	if captureErr == nil && len(pcm.Samples) < limit && runCtx.Err() != nil {
		captureErr = runCtx.Err()
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		captureErr = errors.Join(captureErr, err)
	}
	if captureErr == nil && len(pcm.Samples) < limit {
		captureErr = fmt.Errorf("microphone stopped before the requested duration")
	}
	return pcm, captureErr
}
