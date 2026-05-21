package sections

import (
	"testing"

	"lifx-maestro/internal/analysis"
)

func TestFallbackSectionsUseEarlyBuildAndDrop(t *testing.T) {
	got := fallback(analysis.SongAnalysis{DurationMS: 138183})
	if len(got) != 7 {
		t.Fatalf("sections = %d, want 7", len(got))
	}

	want := []struct {
		start int64
		end   int64
		typ   Type
	}{
		{0, 16581, TypeIntro},
		{16581, 24872, TypeBuild},
		{24872, 69091, TypeDrop},
	}

	for i, section := range want {
		if got[i].StartMS != section.start || got[i].EndMS != section.end || got[i].Type != section.typ {
			t.Fatalf("section %d = %+v, want start=%d end=%d type=%s", i, got[i], section.start, section.end, section.typ)
		}
	}
}
