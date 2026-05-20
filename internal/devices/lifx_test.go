package devices

import "testing"

func TestNormalizePercent(t *testing.T) {
	tests := map[float64]float64{
		0:    0,
		0.5:  50,
		1:    100,
		35:   35,
		120:  100,
		-0.5: 0,
	}

	for input, want := range tests {
		if got := normalizePercent(input); got != want {
			t.Fatalf("normalizePercent(%v) = %v, want %v", input, got, want)
		}
	}
}
