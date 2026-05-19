package timeline

import "testing"

func TestValidateRejectsInvalidEvent(t *testing.T) {
	tl := Timeline{
		Name:       "bad",
		DurationMS: 1000,
		Events: []Event{
			{TimeMS: 1001, Target: "desk", Action: "power_on"},
		},
	}

	if err := tl.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSortEventsIsStable(t *testing.T) {
	tl := Timeline{
		Name: "demo",
		Events: []Event{
			{TimeMS: 1000, Target: "first", Action: "power_on"},
			{TimeMS: 0, Target: "second", Action: "power_on"},
			{TimeMS: 1000, Target: "third", Action: "power_on"},
		},
	}

	tl.SortEvents()

	got := []string{tl.Events[0].Target, tl.Events[1].Target, tl.Events[2].Target}
	want := []string{"second", "first", "third"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target %d = %q, want %q", i, got[i], want[i])
		}
	}
}
