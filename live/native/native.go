// Package native provides an experimental in-process Live analyzer. Python
// remains Maestro's default while numeric and hardware parity are evaluated.
package native

import (
	"context"
	"fmt"
	"sync"

	"github.com/alessio-palumbo/lifx-maestro/internal/audiodsp"
	"github.com/alessio-palumbo/lifx-maestro/live"
)

type Analyzer struct {
	mu  sync.Mutex
	dsp *audiodsp.Analyzer
}

func New() *Analyzer { return &Analyzer{dsp: audiodsp.New()} }

func (a *Analyzer) Analyze(ctx context.Context, w live.PCMWindow) (live.Features, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dsp == nil {
		return live.Features{}, fmt.Errorf("native analyzer is closed")
	}
	f, err := a.dsp.Analyze(ctx, audiodsp.Window{Samples: w.Samples, SampleRate: w.SampleRate, End: w.End})
	if err != nil {
		return live.Features{}, err
	}
	result := live.Features{
		At: f.At, RMSDB: f.RMSDB, RecentRMSDB: f.RecentRMSDB, HasRecentRMS: true,
		LowDB: f.LowDB, MidDB: f.MidDB, HighDB: f.HighDB,
		RecentLowDB: f.RecentLowDB, RecentMidDB: f.RecentMidDB, RecentHighDB: f.RecentHighDB, HasRecentBands: true,
		Onset: f.Onset, OnsetStrength: f.OnsetStrength, TempoBPM: f.TempoBPM, TempoConfidence: f.TempoConfidence, Beat: f.Beat,
	}
	for _, c := range f.TempoCandidates {
		result.TempoCandidates = append(result.TempoCandidates, live.TempoCandidate{BPM: c.BPM, Confidence: c.Confidence, Strength: c.Strength})
	}
	return result, nil
}

func (a *Analyzer) Close() error { a.mu.Lock(); defer a.mu.Unlock(); a.dsp = nil; return nil }

var _ live.Analyzer = (*Analyzer)(nil)
