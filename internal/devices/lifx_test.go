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

func TestMatrixChainLengthDefaultsToOne(t *testing.T) {
	if got := matrixChainLength(lifxdevice.Device{}); got != 1 {
		t.Fatalf("matrixChainLength = %d, want 1", got)
	}
}

func TestMatrixChainLengthUsesDiscoveredChainLength(t *testing.T) {
	device := lifxdevice.Device{}
	device.MatrixProperties.ChainLength = 3

	if got := matrixChainLength(device); got != 3 {
		t.Fatalf("matrixChainLength = %d, want 3", got)
	}
}

func TestDeviceCapabilitiesPreserveMatrixSendDimensions(t *testing.T) {
	device := lifxdevice.Device{LightType: lifxdevice.LightTypeMatrix}
	device.MatrixProperties.Width = 8
	device.MatrixProperties.Height = 8
	device.MatrixProperties.NZones = 64
	device.MatrixProperties.ChainLength = 2

	got := deviceCapabilities(device)
	if got.Kind != DeviceKindMatrix {
		t.Fatalf("kind = %q, want %q", got.Kind, DeviceKindMatrix)
	}
	if got.MatrixWidth != 8 || got.MatrixHeight != 8 || got.ZoneCount != 64 || got.MatrixLength != 2 {
		t.Fatalf("capabilities = %+v, want matrix width=8 height=8 zones=64 length=2", got)
	}
	if got.Surface.Width == 0 || got.Surface.Height == 0 {
		t.Fatalf("surface = %+v, want populated surface dimensions", got.Surface)
	}
}

func TestMatrixLengthFromSurfaceUsesLifxSurfaceChains(t *testing.T) {
	surface := lifxdevice.Surface{
		Width:  16,
		Height: 8,
		Zones:  128,
		Matrix: &lifxdevice.MatrixSurface{Chains: []lifxdevice.MatrixChain{
			{
				Index:       1,
				Bounds:      lifxdevice.Rect{X: 8, Y: 0, Width: 8, Height: 8},
				SendWidth:   8,
				Rows:        []lifxdevice.MatrixRow{{Cols: 7, Offset: 1, HiddenCols: []int{0}}},
				Orientation: lifxdevice.OrientationUpsideDown,
			},
		}},
	}

	if got := matrixLengthFromSurface(surface, 0); got != 1 {
		t.Fatalf("matrixLengthFromSurface = %d, want 1", got)
	}
}
