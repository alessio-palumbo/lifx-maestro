package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestRunDispatchesSortedEvents(t *testing.T) {
	events := []Event{
		{TimeMS: 4, Index: 1, Value: "second"},
		{TimeMS: 0, Index: 0, Value: "first"},
		{TimeMS: 4, Index: 2, Value: "third"},
	}

	var got []string
	s := New(Options{
		PollInterval: time.Millisecond,
		Lookahead:    2 * time.Millisecond,
	})

	if err := s.Run(context.Background(), events, func(ctx context.Context, event Event) {
		got = append(got, event.Value.(string))
	}); err != nil {
		t.Fatal(err)
	}

	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}
