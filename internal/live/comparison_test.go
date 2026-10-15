package live

import (
	"context"
	"testing"
)

func TestComparisonClosesBothAnalyzersOnceOnInvalidInput(t *testing.T) {
	reference, candidate := &closingAnalyzer{}, &closingAnalyzer{}
	_, _, _, err := ComparePCM(context.Background(), "invalid", nil, 16000, EvaluationConfig{Analyzer: reference}, EvaluationConfig{Analyzer: candidate})
	if err == nil {
		t.Fatal("invalid PCM accepted")
	}
	if reference.closes != 1 || candidate.closes != 1 {
		t.Fatalf("close counts = %d/%d", reference.closes, candidate.closes)
	}
}

func TestFeatureComparisonDetectsSmallAndDiscreteDifferences(t *testing.T) {
	a := Features{RMSDB: -42, Onset: true, TempoCandidates: []TempoCandidate{{BPM: 120, Confidence: 0.8}}}
	b := a
	b.RMSDB += 0.1
	b.Onset = false
	b.TempoCandidates = []TempoCandidate{{BPM: 60, Confidence: 0.8}}
	r := compareFeatures([]Features{a}, []Features{b})
	if r.OnsetMismatches != 1 || r.CandidateMismatches != 1 || r.MaxDifference["rms_db"] < 0.09 {
		t.Fatalf("differences not detected: %+v", r)
	}
}
