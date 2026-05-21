package devices

import (
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

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

func TestSplitSelectors(t *testing.T) {
	got := splitSelectors(" tv, desk ,, living room ")
	want := []string{"tv", "desk", "living room"}

	if len(got) != len(want) {
		t.Fatalf("selectors = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selector %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveSelectorMatchesLabelGroupLocationAndSerial(t *testing.T) {
	tvSerial, err := lifxdevice.SerialFromHex("001122334455")
	if err != nil {
		t.Fatal(err)
	}
	deskSerial, err := lifxdevice.SerialFromHex("aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}

	devices := []lifxdevice.Device{
		{Serial: tvSerial, Label: "TV", Group: "Lounge", Location: "Home"},
		{Serial: deskSerial, Label: "Desk", Group: "Office", Location: "Home"},
	}

	if got := resolveSelector("tv", devices); len(got) != 1 || got[0] != tvSerial {
		t.Fatalf("label selector = %#v, want %s", got, tvSerial)
	}
	if got := resolveSelector("office", devices); len(got) != 1 || got[0] != deskSerial {
		t.Fatalf("group selector = %#v, want %s", got, deskSerial)
	}
	if got := resolveSelector("home", devices); len(got) != 2 {
		t.Fatalf("location selector len = %d, want 2", len(got))
	}
	if got := resolveSelector("001122334455", devices); len(got) != 1 || got[0] != tvSerial {
		t.Fatalf("serial selector = %#v, want %s", got, tvSerial)
	}
}
