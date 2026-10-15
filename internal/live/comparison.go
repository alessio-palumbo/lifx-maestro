package live

import (
	"context"
	"fmt"
	"io"
	"math"
	"reflect"
	"sync"
	"time"
)

// AnalyzerComparison measures both analyzers on the same normalized PCM and
// rolling windows, followed by independent instances of the existing tracker
// and generator. Timing excludes process creation, decoding, tracking, and
// generation, but includes initialization deferred to the first Analyze call.
type AnalyzerComparison struct {
	Windows             int                `json:"windows"`
	MaxDifference       map[string]float64 `json:"max_feature_difference"`
	OnsetMismatches     int                `json:"onset_mismatches"`
	BeatMismatches      int                `json:"beat_mismatches"`
	CandidateMismatches int                `json:"candidate_mismatches"`
	DecisionMismatches  int                `json:"decision_mismatches"`
	EventMismatches     int                `json:"event_mismatches"`
	ReferenceAnalysisMS float64            `json:"reference_analysis_ms"`
	NativeAnalysisMS    float64            `json:"native_analysis_ms"`
}

type recordingAnalyzer struct {
	Analyzer
	features  []Features
	elapsed   time.Duration
	closeOnce sync.Once
	closeErr  error
}

func (a *recordingAnalyzer) Close() error {
	a.closeOnce.Do(func() { a.closeErr = a.Analyzer.Close() })
	return a.closeErr
}

func (a *recordingAnalyzer) Analyze(ctx context.Context, w PCMWindow) (Features, error) {
	start := time.Now()
	f, err := a.Analyzer.Analyze(ctx, w)
	a.elapsed += time.Since(start)
	if err == nil {
		a.features = append(a.features, f)
	}
	return f, err
}

// ComparePCM owns both analyzers, including cleanup if the reference run fails.
// Configurations must use matching input/window settings and distinct trackers
// and generators. The caller supplies those so no device access is necessary.
func ComparePCM(ctx context.Context, source string, samples []float32, sampleRate int, reference, candidate EvaluationConfig) (*EvaluationTrace, *EvaluationTrace, *AnalyzerComparison, error) {
	if reference.Analyzer == nil || candidate.Analyzer == nil {
		return nil, nil, nil, fmt.Errorf("comparison requires two analyzers")
	}
	r := &recordingAnalyzer{Analyzer: reference.Analyzer}
	c := &recordingAnalyzer{Analyzer: candidate.Analyzer}
	defer r.Close()
	defer c.Close()
	reference.Analyzer, candidate.Analyzer = r, c
	rTrace, err := EvaluatePCM(ctx, source, samples, sampleRate, reference)
	if err != nil {
		_ = c.Close()
		return nil, nil, nil, err
	}
	cTrace, err := EvaluatePCM(ctx, source, samples, sampleRate, candidate)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(r.features) != len(c.features) {
		return nil, nil, nil, fmt.Errorf("comparison windows differ")
	}
	if rTrace.WindowMS != cTrace.WindowMS || rTrace.HopMS != cTrace.HopMS || rTrace.InputLevelDB != cTrace.InputLevelDB || rTrace.CalibrationMS != cTrace.CalibrationMS || rTrace.PreservedInputLevel != cTrace.PreservedInputLevel {
		return nil, nil, nil, fmt.Errorf("comparison requires matching input, calibration, window, and hop settings")
	}
	report := compareFeatures(r.features, c.features)
	report.ReferenceAnalysisMS = float64(r.elapsed) / float64(time.Millisecond)
	report.NativeAnalysisMS = float64(c.elapsed) / float64(time.Millisecond)
	for i, r := range rTrace.Points {
		c := cTrace.Points[i]
		if r.Active != c.Active || r.Sustained != c.Sustained || r.Onset != c.Onset || r.Beat != c.Beat ||
			r.Dynamics != c.Dynamics || r.SectionChange != c.SectionChange || r.Intent != c.Intent ||
			r.Accent != c.Accent || r.GeneratedEvents != c.GeneratedEvents {
			report.DecisionMismatches++
		}
	}
	report.EventMismatches = int(math.Abs(float64(len(rTrace.Events) - len(cTrace.Events))))
	for i := range min(len(rTrace.Events), len(cTrace.Events)) {
		if !reflect.DeepEqual(rTrace.Events[i], cTrace.Events[i]) {
			report.EventMismatches++
		}
	}
	return rTrace, cTrace, report, nil
}

func compareFeatures(reference, candidate []Features) *AnalyzerComparison {
	r := &AnalyzerComparison{Windows: len(reference), MaxDifference: make(map[string]float64)}
	for i, a := range reference {
		b := candidate[i]
		pairs := []struct {
			name string
			a, b float64
		}{
			{"rms_db", a.RMSDB, b.RMSDB}, {"recent_rms_db", a.RecentRMSDB, b.RecentRMSDB},
			{"low_db", a.LowDB, b.LowDB}, {"mid_db", a.MidDB, b.MidDB}, {"high_db", a.HighDB, b.HighDB},
			{"recent_low_db", a.RecentLowDB, b.RecentLowDB}, {"recent_mid_db", a.RecentMidDB, b.RecentMidDB}, {"recent_high_db", a.RecentHighDB, b.RecentHighDB},
			{"onset_strength", a.OnsetStrength, b.OnsetStrength}, {"tempo_bpm", a.TempoBPM, b.TempoBPM}, {"tempo_confidence", a.TempoConfidence, b.TempoConfidence},
		}
		for _, p := range pairs {
			r.MaxDifference[p.name] = max(r.MaxDifference[p.name], math.Abs(p.a-p.b))
		}
		if a.Onset != b.Onset {
			r.OnsetMismatches++
		}
		if a.Beat != b.Beat {
			r.BeatMismatches++
		}
		if !reflect.DeepEqual(a.TempoCandidates, b.TempoCandidates) {
			r.CandidateMismatches++
		}
	}
	return r
}

func WriteAnalyzerComparison(out io.Writer, r *AnalyzerComparison) {
	fmt.Fprintf(out, "\nPython / native comparison (%d identical windows)\n", r.Windows)
	for _, name := range []string{"rms_db", "recent_rms_db", "low_db", "mid_db", "high_db", "recent_low_db", "recent_mid_db", "recent_high_db", "onset_strength", "tempo_bpm", "tempo_confidence"} {
		fmt.Fprintf(out, "max %-17s %.6f\n", name, r.MaxDifference[name])
	}
	fmt.Fprintf(out, "onset / beat mismatches     %d / %d\ncandidate list mismatches   %d\nshow decision mismatches    %d\ntimeline event mismatches   %d\nanalysis time Python / Go   %.1f / %.1f ms (first call included)\n",
		r.OnsetMismatches, r.BeatMismatches, r.CandidateMismatches, r.DecisionMismatches, r.EventMismatches, r.ReferenceAnalysisMS, r.NativeAnalysisMS)
}
