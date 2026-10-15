// Package audiodsp extracts causal musical features from mono PCM. It has no
// lighting, device, application, audio-capture, or process dependencies.
package audiodsp

import (
	"context"
	"fmt"
	"math"
	"math/cmplx"
	"slices"
	"time"

	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	RecentDuration       = 100 * time.Millisecond
	OnsetFrameDuration   = 25 * time.Millisecond
	OnsetRefractory      = 250 * time.Millisecond
	TempoHistoryDuration = 8 * time.Second
	MinimumTempoHistory  = 2 * time.Second
	onsetReleaseRatio    = 0.55
)

type Window struct {
	Samples    []float32
	SampleRate int
	End        time.Duration
}

type TempoCandidate struct {
	BPM        float64
	Confidence float64
	Strength   float64
}

type Features struct {
	At                                     time.Duration
	RMSDB                                  float64
	RecentRMSDB                            float64
	LowDB, MidDB, HighDB                   float64
	RecentLowDB, RecentMidDB, RecentHighDB float64
	OnsetStrength                          float64
	Onset                                  bool
	TempoBPM, TempoConfidence              float64
	TempoCandidates                        []TempoCandidate
	Beat                                   bool
}

// Analyzer maintains onset/tempo history for one stream. Calls are sequential;
// create a new instance for each stream or sample-rate change. It never retains
// input PCM. FFT workspaces and history are bounded. Discard a canceled run.
type Analyzer struct {
	whole, recent, onset spectrum
	previous             []float64
	flux, times          []float64
	lastAt               time.Duration
	started              bool
	sampleRate           int
	lastOnset            float64
	armed                bool
	tempo                float64
}

func New() *Analyzer { return &Analyzer{lastOnset: -1e9, armed: true} }

func (a *Analyzer) Analyze(ctx context.Context, w Window) (Features, error) {
	if err := ctx.Err(); err != nil {
		return Features{}, err
	}
	if len(w.Samples) == 0 || w.SampleRate <= 0 || w.End < 0 {
		return Features{}, fmt.Errorf("analysis requires nonempty PCM, a positive sample rate, and a nonnegative timestamp")
	}
	if a.sampleRate != 0 && a.sampleRate != w.SampleRate {
		return Features{}, fmt.Errorf("sample rate changed; create a new analyzer")
	}
	if a.started && w.End <= a.lastAt {
		return Features{}, fmt.Errorf("analysis timestamps must increase")
	}
	for _, v := range w.Samples {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return Features{}, fmt.Errorf("PCM contains a nonfinite sample")
		}
	}
	a.sampleRate = w.SampleRate
	whole, wholeGain := a.whole.compute(w.Samples)
	recentSize := min(len(w.Samples), max(1, int(math.RoundToEven(float64(w.SampleRate)*RecentDuration.Seconds()))))
	recentSamples := w.Samples[len(w.Samples)-recentSize:]
	recent, recentGain := a.recent.compute(recentSamples)
	low, mid, high := bands(whole, len(w.Samples), w.SampleRate, wholeGain)
	rLow, rMid, rHigh := bands(recent, recentSize, w.SampleRate, recentGain)
	flux, onset, err := a.analyzeOnset(ctx, w)
	if err != nil {
		return Features{}, err
	}
	tempo, confidence, candidates, err := a.estimateTempo(ctx)
	if err != nil {
		return Features{}, err
	}
	if tempo > 0 {
		if a.tempo <= 0 {
			a.tempo = tempo
		} else {
			a.tempo = a.tempo*0.82 + tempo*0.18
		}
	}
	return Features{
		At:    time.Duration(math.RoundToEven(float64(w.End)/float64(time.Millisecond))) * time.Millisecond,
		RMSDB: amplitudeDB(rms(w.Samples)), RecentRMSDB: amplitudeDB(rms(recentSamples)),
		LowDB: low, MidDB: mid, HighDB: high, RecentLowDB: rLow, RecentMidDB: rMid, RecentHighDB: rHigh,
		OnsetStrength: rounded(flux, 6), Onset: onset, TempoBPM: rounded(a.tempo, 3),
		TempoConfidence: rounded(confidence, 4), TempoCandidates: candidates, Beat: onset && confidence >= 0.35,
	}, nil
}

// spectrum reuses one FFT plan and Hann workspace at its current input size.
type spectrum struct {
	fft                      *fourier.FFT
	window, input, magnitude []float64
	coefficients             []complex128
	gain                     float64
}

func (s *spectrum) compute(samples []float32) ([]float64, float64) {
	n := len(samples)
	if len(s.window) != n {
		s.fft = fourier.NewFFT(n)
		s.window = make([]float64, n)
		s.input = make([]float64, n)
		s.magnitude = make([]float64, n/2+1)
		s.coefficients = make([]complex128, n/2+1)
		var sum float64
		for i := range s.window {
			v := 1.0
			if n > 1 {
				v = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
			}
			s.window[i] = v
			sum += v * v
		}
		s.gain = math.Sqrt(sum / float64(n))
	}
	for i, v := range samples {
		s.input[i] = float64(v) * s.window[i]
	}
	s.fft.Coefficients(s.coefficients, s.input)
	for i, c := range s.coefficients {
		s.magnitude[i] = cmplx.Abs(c)
	}
	return s.magnitude, s.gain
}

func bands(magnitude []float64, n, sampleRate int, gain float64) (float64, float64, float64) {
	return bandDB(magnitude, n, sampleRate, gain, 20, 250),
		bandDB(magnitude, n, sampleRate, gain, 250, 4000),
		bandDB(magnitude, n, sampleRate, gain, 4000, min(12000, float64(sampleRate)/2))
}

func bandDB(magnitude []float64, n, sampleRate int, gain, low, high float64) float64 {
	var sum float64
	found := false
	for i, v := range magnitude {
		frequency := float64(i) * float64(sampleRate) / float64(n)
		if frequency >= low && frequency < high {
			sum += v * v
			found = true
		}
	}
	if !found {
		return -120
	}
	// Preserve Python's rfft-derived sample count, including odd-length inputs,
	// so this migration does not silently alter existing band calibration.
	count := float64(max((len(magnitude)-1)*2, 1))
	return amplitudeDB(math.Sqrt(2*sum/(count*count)) / max(gain, 1e-6))
}

func rms(samples []float32) float64 {
	var sum float64
	for _, v := range samples {
		x := float64(v)
		sum += x * x
	}
	return math.Sqrt(sum / float64(len(samples)))
}
func amplitudeDB(v float64) float64 { return rounded(20*math.Log10(max(v, 1e-6)), 4) }
func rounded(v float64, places int) float64 {
	scale := math.Pow10(places)
	return math.RoundToEven(v*scale) / scale
}
func clamp(v, low, high float64) float64 { return max(low, min(high, v)) }
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copy := slices.Clone(values)
	slices.Sort(copy)
	m := len(copy) / 2
	if len(copy)%2 != 0 {
		return copy[m]
	}
	return (copy[m-1] + copy[m]) / 2
}

func (a *Analyzer) analyzeOnset(ctx context.Context, w Window) (float64, bool, error) {
	elapsed := RecentDuration.Seconds()
	if a.started {
		// Match the reference's subtraction of floating-point timestamps. Using
		// exact duration subtraction changes decisions at the 250 ms refractory
		// boundary, despite otherwise identical rounded numeric features.
		elapsed = w.End.Seconds() - a.lastAt.Seconds()
	}
	a.lastAt, a.started = w.End, true
	if elapsed <= 0 || elapsed > 0.25 {
		elapsed = RecentDuration.Seconds()
	}
	frameSize := max(16, int(math.RoundToEven(float64(w.SampleRate)*OnsetFrameDuration.Seconds())))
	newestSize := min(len(w.Samples), max(frameSize, int(math.RoundToEven(float64(w.SampleRate)*elapsed))))
	newest := w.Samples[len(w.Samples)-newestSize:]
	count := max(1, (len(newest)+frameSize-1)/frameSize)
	padding := count*frameSize - len(newest)
	frame := make([]float32, frameSize)
	strongest := 0.0
	accepted := false
	for index := range count {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		clear(frame)
		for i := range frame {
			at := index*frameSize + i - padding
			if at >= 0 && at < len(newest) {
				frame[i] = newest[at]
			}
		}
		magnitude, _ := a.onset.compute(frame)
		flux := 0.0
		if len(a.previous) == len(magnitude) {
			var positive, reference float64
			for i, v := range magnitude {
				positive += max(v-a.previous[i], 0)
				reference += max(v, a.previous[i])
			}
			flux = positive / max(reference, 1e-12)
		}
		a.previous = append(a.previous[:0], magnitude...)
		strongest = max(strongest, flux)
		upper := 0.08
		if len(a.flux) >= 32 {
			var mean, variance float64
			for _, v := range a.flux {
				mean += v
			}
			mean /= float64(len(a.flux))
			for _, v := range a.flux {
				variance += (v - mean) * (v - mean)
			}
			upper = max(0.025, median(a.flux)+math.Sqrt(variance/float64(len(a.flux)))*2.2)
		}
		lower := max(0.0125, upper*onsetReleaseRatio)
		frameAt := w.End.Seconds() - elapsed + min(elapsed, float64((index+1)*frameSize)/float64(w.SampleRate))
		if !a.armed && flux <= lower {
			a.armed = true
		}
		if a.armed && flux >= upper && frameAt-a.lastOnset >= OnsetRefractory.Seconds() {
			accepted, a.armed, a.lastOnset = true, false, frameAt
		}
		limit := int(TempoHistoryDuration / OnsetFrameDuration)
		if len(a.flux) == limit {
			copy(a.flux, a.flux[1:])
			a.flux = a.flux[:limit-1]
			copy(a.times, a.times[1:])
			a.times = a.times[:limit-1]
		}
		a.flux = append(a.flux, flux)
		a.times = append(a.times, frameAt)
	}
	return strongest, accepted, nil
}

func (a *Analyzer) estimateTempo(ctx context.Context) (float64, float64, []TempoCandidate, error) {
	if len(a.flux) < int(MinimumTempoHistory/OnsetFrameDuration) {
		return 0, 0, nil, nil
	}
	hops := make([]float64, len(a.times)-1)
	for i := range hops {
		hops[i] = a.times[i+1] - a.times[i]
	}
	hop := median(hops)
	if hop <= 0 {
		return 0, 0, nil, nil
	}
	envelope := slices.Clone(a.flux)
	baseline := median(envelope)
	var mean float64
	for i, v := range envelope {
		envelope[i] = max(v-baseline, 0)
		mean += envelope[i]
	}
	mean /= float64(len(envelope))
	var energy float64
	for i := range envelope {
		envelope[i] -= mean
		energy += envelope[i] * envelope[i]
	}
	if energy <= 1e-12 {
		return 0, 0, nil, nil
	}
	minLag := max(1, int(math.Floor(60/(190*hop))))
	maxLag := min(len(envelope)-2, int(math.Ceil(60/(55*hop))))
	if maxLag < minLag {
		return 0, 0, nil, nil
	}
	correlations := make([]float64, maxLag+2)
	correlations[0] = 1
	for lag := 1; lag < len(correlations); lag++ {
		if err := ctx.Err(); err != nil {
			return 0, 0, nil, err
		}
		var dot, left, right float64
		for i := 0; i < len(envelope)-lag; i++ {
			x, y := envelope[i], envelope[i+lag]
			dot += x * y
			left += x * x
			right += y * y
		}
		denominator := math.Sqrt(left * right)
		if denominator > 1e-12 {
			correlations[lag] = dot / denominator
		}
	}
	best := minLag
	for lag := minLag + 1; lag <= maxLag; lag++ {
		if correlations[lag] > correlations[best] {
			best = lag
		}
	}
	bestLag := float64(best)
	denominator := correlations[best-1] - 2*correlations[best] + correlations[best+1]
	if math.Abs(denominator) > 1e-12 {
		bestLag += clamp(0.5*(correlations[best-1]-correlations[best+1])/denominator, -0.5, 0.5)
	}
	confidence := clamp((correlations[best]-0.08)/0.42, 0, 1) * min(1, float64(len(envelope))/max(bestLag*6, 1))
	ranked := make([]int, maxLag-minLag+1)
	for i := range ranked {
		ranked[i] = minLag + i
	}
	slices.SortFunc(ranked, func(i, j int) int {
		if correlations[i] > correlations[j] {
			return -1
		}
		if correlations[i] < correlations[j] {
			return 1
		}
		return j - i
	})
	var candidates []TempoCandidate
	for _, lag := range ranked {
		bpm := 60 / (float64(lag) * hop)
		duplicate := false
		for _, c := range candidates {
			if math.Abs(math.Log2(bpm/c.BPM)) < 0.035 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		support := min(1, float64(len(envelope))/max(float64(lag)*6, 1))
		candidates = append(candidates, TempoCandidate{BPM: rounded(bpm, 3), Confidence: rounded(clamp((correlations[lag]-0.08)/0.42, 0, 1)*support, 4), Strength: rounded(correlations[lag], 4)})
		if len(candidates) == 5 {
			break
		}
	}
	return 60 / (bestLag * hop), confidence, candidates, nil
}
