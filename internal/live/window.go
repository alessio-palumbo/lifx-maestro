package live

import "time"

type WindowBuffer struct {
	sampleRate int
	windowSize int
	hopSize    int
	buffer     []float32
	consumed   int64
	nextEnd    int
}

func NewWindowBuffer(sampleRate int, window, hop time.Duration) *WindowBuffer {
	windowSize := durationSamples(sampleRate, window)
	hopSize := durationSamples(sampleRate, hop)
	if windowSize < 1 {
		windowSize = 1
	}
	if hopSize < 1 {
		hopSize = 1
	}
	return &WindowBuffer{
		sampleRate: sampleRate,
		windowSize: windowSize,
		hopSize:    hopSize,
		nextEnd:    windowSize,
	}
}

func (b *WindowBuffer) Push(chunk PCMChunk) []PCMWindow {
	if chunk.SampleRate != b.sampleRate || len(chunk.Samples) == 0 {
		return nil
	}
	b.buffer = append(b.buffer, chunk.Samples...)

	var windows []PCMWindow
	for len(b.buffer) >= b.nextEnd {
		start := b.nextEnd - b.windowSize
		samples := append([]float32(nil), b.buffer[start:b.nextEnd]...)
		absoluteStart := b.consumed + int64(start)
		absoluteEnd := b.consumed + int64(b.nextEnd)
		windows = append(windows, PCMWindow{
			Samples:    samples,
			SampleRate: b.sampleRate,
			Start:      sampleDuration(b.sampleRate, absoluteStart),
			End:        sampleDuration(b.sampleRate, absoluteEnd),
		})
		b.nextEnd += b.hopSize
	}

	// Retain exactly the history needed by the next overlapping window.
	drop := b.nextEnd - b.windowSize
	if drop > 0 {
		b.buffer = append(b.buffer[:0], b.buffer[drop:]...)
		b.consumed += int64(drop)
		b.nextEnd -= drop
	}
	return windows
}

func durationSamples(sampleRate int, duration time.Duration) int {
	if sampleRate <= 0 || duration <= 0 {
		return 0
	}
	return int((int64(sampleRate)*duration.Nanoseconds() + int64(time.Second) - 1) / int64(time.Second))
}

func sampleDuration(sampleRate int, samples int64) time.Duration {
	if sampleRate <= 0 || samples <= 0 {
		return 0
	}
	return time.Duration(samples * int64(time.Second) / int64(sampleRate))
}
