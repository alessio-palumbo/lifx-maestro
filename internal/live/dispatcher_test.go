package live

import (
	"context"
	"sync"
	"testing"
	"time"

	"lifx-maestro/internal/timeline"
)

type blockingExecutor struct {
	started chan timeline.Event
	release chan struct{}

	mu     sync.Mutex
	events []timeline.Event
}

func (e *blockingExecutor) ExecuteEvent(_ int, event timeline.Event) error {
	e.started <- event
	<-e.release
	e.mu.Lock()
	e.events = append(e.events, event)
	e.mu.Unlock()
	return nil
}

func TestEnqueueLatestReplacesPendingEvent(t *testing.T) {
	queue := make(chan timeline.Event, 1)
	enqueueLatest(queue, timeline.Event{Target: "strip", TimeMS: 2})
	enqueueLatest(queue, timeline.Event{Target: "strip", TimeMS: 3})

	if latest := <-queue; latest.TimeMS != 3 {
		t.Fatalf("pending event time = %d, want latest 3", latest.TimeMS)
	}
}

func TestDispatcherRunsTargetsConcurrently(t *testing.T) {
	executor := &blockingExecutor{started: make(chan timeline.Event, 4), release: make(chan struct{}, 4)}
	dispatcher := NewDispatcher(executor, nil)
	input := make(chan []timeline.Event)
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(context.Background(), input) }()

	input <- []timeline.Event{{Target: "lamp"}, {Target: "strip"}}
	targets := map[string]bool{}
	for range 2 {
		select {
		case event := <-executor.started:
			targets[event.Target] = true
		case <-time.After(time.Second):
			t.Fatal("separate target was blocked")
		}
	}
	if !targets["lamp"] || !targets["strip"] {
		t.Fatalf("started targets = %v", targets)
	}

	executor.release <- struct{}{}
	executor.release <- struct{}{}
	close(input)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
