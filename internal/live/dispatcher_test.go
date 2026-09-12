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
	dispatcher := NewDispatcher(nil, nil)
	queue := make(chan timeline.Event, 1)
	dispatcher.enqueueLatest(queue, timeline.Event{Target: "strip", TimeMS: 2})
	dispatcher.enqueueLatest(queue, timeline.Event{Target: "strip", TimeMS: 3})

	if latest := <-queue; latest.TimeMS != 3 {
		t.Fatalf("pending event time = %d, want latest 3", latest.TimeMS)
	}
	if got := dispatcher.Stats().Replaced; got != 1 {
		t.Fatalf("replaced events = %d, want 1", got)
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
	if got := dispatcher.Stats().Sent; got != 2 {
		t.Fatalf("sent events = %d, want 2", got)
	}
}
