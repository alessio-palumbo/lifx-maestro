package live

import (
	"reflect"
	"testing"
	"time"
)

func TestWindowBufferProducesOverlappingWindows(t *testing.T) {
	buffer := NewWindowBuffer(10, 400*time.Millisecond, 200*time.Millisecond)
	windows := buffer.Push(PCMChunk{SampleRate: 10, Samples: []float32{0, 1, 2, 3, 4, 5, 6, 7}})

	if len(windows) != 3 {
		t.Fatalf("windows = %d, want 3", len(windows))
	}
	want := [][]float32{{0, 1, 2, 3}, {2, 3, 4, 5}, {4, 5, 6, 7}}
	for i := range want {
		if !reflect.DeepEqual(windows[i].Samples, want[i]) {
			t.Fatalf("window %d = %v, want %v", i, windows[i].Samples, want[i])
		}
	}
	if windows[2].Start != 400*time.Millisecond || windows[2].End != 800*time.Millisecond {
		t.Fatalf("last window = %s..%s, want 400ms..800ms", windows[2].Start, windows[2].End)
	}
}

func TestWindowBufferCarriesHistoryAcrossChunks(t *testing.T) {
	buffer := NewWindowBuffer(10, 400*time.Millisecond, 200*time.Millisecond)
	if windows := buffer.Push(PCMChunk{SampleRate: 10, Samples: []float32{0, 1, 2}}); len(windows) != 0 {
		t.Fatalf("early windows = %d, want 0", len(windows))
	}
	windows := buffer.Push(PCMChunk{SampleRate: 10, Samples: []float32{3, 4, 5}})
	if len(windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(windows))
	}
	if !reflect.DeepEqual(windows[1].Samples, []float32{2, 3, 4, 5}) {
		t.Fatalf("overlap = %v", windows[1].Samples)
	}
}
