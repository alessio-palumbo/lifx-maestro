package devices

type ColorParams struct {
	Hue        float64
	Saturation float64
	Brightness float64
	Kelvin     int
	DurationMS int64
}

type DeviceController interface {
	PowerOn(target string) error
	PowerOff(target string) error
	SetColor(target string, params ColorParams) error
}

type StateRestorer interface {
	CaptureState(target string) error
	RestoreState() error
}
