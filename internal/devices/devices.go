package devices

import lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"

type ColorParams struct {
	Hue        float64
	Saturation float64
	Brightness float64
	Kelvin     int
	DurationMS int64
}

type ZoneColorParams struct {
	Index      int
	Hue        float64
	Saturation float64
	Brightness float64
	Kelvin     int
}

type MatrixColorParams struct {
	X          int
	Y          int
	Hue        float64
	Saturation float64
	Brightness float64
	Kelvin     int
}

type DeviceKind string

const (
	DeviceKindSingleZone DeviceKind = "single_zone"
	DeviceKindMultiZone  DeviceKind = "multi_zone"
	DeviceKindMatrix     DeviceKind = "matrix"
)

type DeviceCapabilities struct {
	Kind         DeviceKind
	HasColor     bool
	HasKelvin    bool
	ZoneCount    int
	MatrixWidth  int
	MatrixHeight int
	MatrixLength int
	Surface      lifxdevice.Surface
}

type DeviceInfo struct {
	ID           string
	Label        string
	Group        string
	Location     string
	Capabilities DeviceCapabilities
}

type DeviceController interface {
	PowerOn(target string) error
	PowerOff(target string) error
	SetColor(target string, params ColorParams) error
	SetZoneColors(target string, zones []ZoneColorParams, durationMS int64) error
	SetMatrixColors(target string, pixels []MatrixColorParams, width, height int, durationMS int64) error
}

type CapabilityProvider interface {
	Devices() ([]DeviceInfo, error)
}

type StateRestorer interface {
	CaptureState(target string) error
	RestoreState() error
}
