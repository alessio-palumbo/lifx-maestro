package devices

import (
	"fmt"
	"strings"
	"time"

	"github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
)

const discoverySettlingDelay = time.Second

type LifxDeviceController struct {
	controller *controller.Controller
	snapshots  []stateSnapshot
}

type stateSnapshot struct {
	serial    lifxdevice.Serial
	poweredOn bool
	color     lifxdevice.Color
}

func NewLifxDeviceController() (*LifxDeviceController, error) {
	ctrl, err := controller.New()
	if err != nil {
		return nil, err
	}

	// controller.New starts discovery asynchronously; give responses time to seed sessions
	// before deterministic playback begins.
	time.Sleep(discoverySettlingDelay)

	return &LifxDeviceController{controller: ctrl}, nil
}

func (l *LifxDeviceController) Close() error {
	if l.controller == nil {
		return nil
	}
	return l.controller.Close()
}

func (l *LifxDeviceController) PowerOn(target string) error {
	return l.send(target, messages.SetPowerOn())
}

func (l *LifxDeviceController) PowerOff(target string) error {
	return l.send(target, messages.SetPowerOff())
}

func (l *LifxDeviceController) SetColor(target string, params ColorParams) error {
	hue := params.Hue
	saturation := normalizePercent(params.Saturation)
	brightness := normalizePercent(params.Brightness)
	kelvin := uint16(params.Kelvin)
	duration := time.Duration(params.DurationMS) * time.Millisecond

	return l.send(target, messages.SetColor(
		&hue,
		&saturation,
		&brightness,
		&kelvin,
		duration,
		0,
	))
}

func (l *LifxDeviceController) CaptureState(target string) error {
	if l.controller == nil {
		return fmt.Errorf("lifx controller is not initialized")
	}

	serials, err := l.resolveTarget(target)
	if err != nil {
		return err
	}

	serialSet := make(map[lifxdevice.Serial]bool, len(serials))
	for _, serial := range serials {
		serialSet[serial] = true
	}

	devices := l.controller.GetDevices()
	snapshots := make([]stateSnapshot, 0, len(serials))
	for _, device := range devices {
		if !serialSet[device.Serial] {
			continue
		}
		snapshots = append(snapshots, stateSnapshot{
			serial:    device.Serial,
			poweredOn: device.PoweredOn,
			color:     device.Color,
		})
	}
	if len(snapshots) == 0 {
		return fmt.Errorf("no LIFX device states captured for target %q", target)
	}

	l.snapshots = snapshots
	return nil
}

func (l *LifxDeviceController) RestoreState() error {
	if l.controller == nil || len(l.snapshots) == 0 {
		return nil
	}

	var restoreErr error
	for _, snapshot := range l.snapshots {
		target := snapshot.serial.String()
		if err := l.SetColor(target, ColorParams{
			Hue:        snapshot.color.Hue,
			Saturation: snapshot.color.Saturation,
			Brightness: snapshot.color.Brightness,
			Kelvin:     int(snapshot.color.Kelvin),
			DurationMS: 500,
		}); err != nil && restoreErr == nil {
			restoreErr = err
		}

		var err error
		if snapshot.poweredOn {
			err = l.PowerOn(target)
		} else {
			err = l.PowerOff(target)
		}
		if err != nil && restoreErr == nil {
			restoreErr = err
		}
	}
	return restoreErr
}

func (l *LifxDeviceController) Devices() ([]DeviceInfo, error) {
	if l.controller == nil {
		return nil, fmt.Errorf("lifx controller is not initialized")
	}

	discovered := l.controller.GetDevices()
	infos := make([]DeviceInfo, 0, len(discovered))
	for _, device := range discovered {
		infos = append(infos, DeviceInfo{
			ID:       device.Serial.String(),
			Label:    device.Label,
			Group:    device.Group,
			Location: device.Location,
			Capabilities: DeviceCapabilities{
				Kind:         deviceKind(device),
				HasColor:     device.ColorProperties.HasColor,
				HasKelvin:    device.ColorProperties.TemperatureRange.Min > 0 || device.ColorProperties.TemperatureRange.Max > 0,
				ZoneCount:    zoneCount(device),
				MatrixWidth:  device.MatrixProperties.Width,
				MatrixHeight: device.MatrixProperties.Height,
			},
		})
	}
	return infos, nil
}

func (l *LifxDeviceController) send(target string, msg *protocol.Message) error {
	if l.controller == nil {
		return fmt.Errorf("lifx controller is not initialized")
	}

	serials, err := l.resolveTarget(target)
	if err != nil {
		return err
	}
	for _, serial := range serials {
		if err := l.controller.Send(serial, msg); err != nil {
			return fmt.Errorf("send to %s: %w", serial, err)
		}
	}
	return nil
}

func (l *LifxDeviceController) resolveTarget(target string) ([]lifxdevice.Serial, error) {
	key := strings.ToLower(strings.TrimSpace(target))
	if key == "" {
		return nil, fmt.Errorf("target is required")
	}

	devices := l.controller.GetDevices()
	if len(devices) == 0 {
		return nil, fmt.Errorf("no LIFX devices discovered")
	}

	seen := make(map[lifxdevice.Serial]bool)
	var serials []lifxdevice.Serial
	add := func(serial lifxdevice.Serial) {
		if !seen[serial] {
			seen[serial] = true
			serials = append(serials, serial)
		}
	}

	if key == "all" {
		for _, device := range devices {
			add(device.Serial)
		}
		return serials, nil
	}

	if serial, err := lifxdevice.SerialFromHex(key); err == nil {
		for _, device := range devices {
			if device.Serial == serial {
				return []lifxdevice.Serial{serial}, nil
			}
		}
	}

	for _, device := range devices {
		if strings.ToLower(device.Label) == key ||
			strings.ToLower(device.Group) == key ||
			strings.ToLower(device.Location) == key {
			add(device.Serial)
		}
	}

	if len(serials) == 0 {
		return nil, fmt.Errorf("no LIFX devices match target %q", target)
	}

	return serials, nil
}

func normalizePercent(value float64) float64 {
	if value >= 0 && value <= 1 {
		return value * 100
	}
	return clamp(value, 0, 100)
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func deviceKind(device lifxdevice.Device) DeviceKind {
	switch device.LightType.String() {
	case "multi_zone":
		return DeviceKindMultiZone
	case "matrix":
		return DeviceKindMatrix
	default:
		return DeviceKindSingleZone
	}
}

func zoneCount(device lifxdevice.Device) int {
	switch deviceKind(device) {
	case DeviceKindMultiZone:
		return len(device.MultizoneProperties.Zones)
	case DeviceKindMatrix:
		if device.MatrixProperties.NZones > 0 {
			return device.MatrixProperties.NZones
		}
		return device.MatrixProperties.Width * device.MatrixProperties.Height
	default:
		return 1
	}
}
