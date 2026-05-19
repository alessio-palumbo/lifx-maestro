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
	return value
}
