package main

import (
	"context"
	"testing"
	"time"

	livemode "github.com/alessio-palumbo/lifx-maestro/internal/live"
	"github.com/alessio-palumbo/lifx-maestro/live/native"
)

func TestNativeLiveAnalyzerSkipsPythonWarmup(t *testing.T) {
	app := &App{analyzerWarmupDone: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		analyzer, err := app.newLiveAnalyzer(context.Background(), "native")
		if err != nil {
			t.Errorf("create native analyzer: %v", err)
			return
		}
		defer analyzer.Close()
		if _, ok := analyzer.(*native.Analyzer); !ok {
			t.Errorf("unexpected analyzer %T", analyzer)
		}
		_, err = analyzer.Analyze(context.Background(), livemode.PCMWindow{
			Samples: make([]float32, 8000), SampleRate: 16000, End: 500 * time.Millisecond,
		})
		if err != nil {
			t.Errorf("analyze silence: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(app.analyzerWarmupDone)
		<-done
		t.Fatal("native analyzer waited for Python warmup")
	}
}

func TestStartLiveRejectsUnknownAnalyzer(t *testing.T) {
	if err := (&App{}).StartLive(LiveRequest{Analyzer: "unknown"}); err == nil {
		t.Fatal("expected unsupported analyzer error")
	}
}
