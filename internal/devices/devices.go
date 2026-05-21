package devices

type ColorParams struct {
	Hue        float64
	Saturation float64
	Brightness float64
	Kelvin     int
	DurationMS int64
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
}

type CapabilityProvider interface {
	Devices() ([]DeviceInfo, error)
}

type StateRestorer interface {
	CaptureState(target string) error
	RestoreState() error
}
