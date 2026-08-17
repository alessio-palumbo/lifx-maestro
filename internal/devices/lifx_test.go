package devices

import (
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
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

func TestDeviceCapabilitiesIdentifySwitches(t *testing.T) {
	device := lifxdevice.Device{Type: lifxdevice.DeviceTypeSwitch}

	got := deviceCapabilities(device)
	if got.Kind != DeviceKindSwitch {
		t.Fatalf("kind = %q, want %q", got.Kind, DeviceKindSwitch)
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

func TestCloneMatrixChainsDeepCopiesColors(t *testing.T) {
	chains := [][]packets.LightHsbk{
		{{Hue: 1, Saturation: 2, Brightness: 3, Kelvin: 3500}},
		{{Hue: 4, Saturation: 5, Brightness: 6, Kelvin: 4000}},
	}

	got := cloneMatrixChains(chains)
	chains[0][0].Hue = 99

	if got[0][0].Hue != 1 {
		t.Fatalf("cloned hue = %d, want original 1", got[0][0].Hue)
	}
	if got[1][0].Kelvin != 4000 {
		t.Fatalf("cloned kelvin = %d, want 4000", got[1][0].Kelvin)
	}
}

func TestCloneSpatialStatePreservesZeroBuffers(t *testing.T) {
	zeroColors := []packets.LightHsbk{{}, {}}

	if got := cloneHSBKs(zeroColors); len(got) != len(zeroColors) {
		t.Fatalf("cloneHSBKs zero buffer len = %d, want %d", len(got), len(zeroColors))
	}
	if got := cloneMatrixChains([][]packets.LightHsbk{zeroColors}); len(got) != 1 || len(got[0]) != len(zeroColors) {
		t.Fatalf("cloneMatrixChains zero buffer = %#v, want preserved chain", got)
	}
}

func TestMatrixRestoreWidth(t *testing.T) {
	if got := matrixRestoreWidth(5, 64); got != 5 {
		t.Fatalf("matrixRestoreWidth with explicit width = %d, want 5", got)
	}
	if got := matrixRestoreWidth(0, 64); got != 8 {
		t.Fatalf("matrixRestoreWidth for tile colors = %d, want 8", got)
	}
	if got := matrixRestoreWidth(0, 10); got != 10 {
		t.Fatalf("matrixRestoreWidth fallback = %d, want 10", got)
	}
}

func TestMatrixStateReadyRequiresPoweredOnMatrixBuffers(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("001122334455")
	if err != nil {
		t.Fatal(err)
	}
	selected := map[lifxdevice.Serial]bool{serial: true}
	device := lifxdevice.Device{
		Serial:    serial,
		LightType: lifxdevice.LightTypeMatrix,
		PoweredOn: true,
	}
	device.MatrixProperties.Width = 8
	device.MatrixProperties.Height = 8
	device.MatrixProperties.ChainLength = 1

	if matrixStateReady(selected, []lifxdevice.Device{device}) {
		t.Fatal("matrix state should not be ready without chain colors")
	}

	device.MatrixProperties.ChainZones = [][]packets.LightHsbk{{{}, {}}}
	if !matrixStateReady(selected, []lifxdevice.Device{device}) {
		t.Fatal("matrix state should be ready with captured chain colors, even when empty")
	}
}

func TestMatrixStateReadyIgnoresPoweredOffMatrix(t *testing.T) {
	serial, err := lifxdevice.SerialFromHex("001122334455")
	if err != nil {
		t.Fatal(err)
	}
	selected := map[lifxdevice.Serial]bool{serial: true}
	device := lifxdevice.Device{
		Serial:    serial,
		LightType: lifxdevice.LightTypeMatrix,
		PoweredOn: false,
	}
	device.MatrixProperties.ChainLength = 1

	if !matrixStateReady(selected, []lifxdevice.Device{device}) {
		t.Fatal("powered-off matrix should not block capture")
	}
}

func TestMatrixSnapshotIdentifiesMatrixDevice(t *testing.T) {
	device := lifxdevice.Device{LightType: lifxdevice.LightTypeMatrix}
	snapshot := stateSnapshot{
		kind: deviceKindFromDevice(device),
	}

	if snapshot.kind != DeviceKindMatrix {
		t.Fatalf("snapshot kind = %q, want %q", snapshot.kind, DeviceKindMatrix)
	}
}

func TestRestorableStateMessagesRequestMatrixChainBeforePixels(t *testing.T) {
	device := lifxdevice.Device{LightType: lifxdevice.LightTypeMatrix}

	got := restorableStateMessages(device)

	if !hasPayload(got, uint16(packets.PayloadTypeTileGetDeviceChain)) {
		t.Fatalf("messages should request matrix chain metadata before pixels")
	}
	if hasPayload(got, uint16(packets.PayloadTypeTileGet64)) {
		t.Fatalf("messages should not request matrix pixels without chain metadata")
	}
}

func TestRestorableStateMessagesRequestMatrixPixels(t *testing.T) {
	device := lifxdevice.Device{LightType: lifxdevice.LightTypeMatrix}
	device.MatrixProperties.Width = 8
	device.MatrixProperties.Height = 8
	device.MatrixProperties.ChainLength = 2
	device.MatrixProperties.StatePackets = 1

	got := restorableStateMessages(device)

	if !hasPayload(got, uint16(packets.PayloadTypeTileGet64)) {
		t.Fatalf("messages should request matrix pixels when chain metadata is known")
	}
	if countPayload(got, uint16(packets.PayloadTypeTileGet64)) != 2 {
		t.Fatalf("TileGet64 message count = %d, want 2", countPayload(got, uint16(packets.PayloadTypeTileGet64)))
	}
}

func hasPayload(messages []*protocol.Message, payloadType uint16) bool {
	return countPayload(messages, payloadType) > 0
}

func countPayload(messages []*protocol.Message, payloadType uint16) int {
	count := 0
	for _, msg := range messages {
		if msg.Type() == payloadType {
			count++
		}
	}
	return count
}
