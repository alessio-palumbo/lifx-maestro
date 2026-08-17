package devices

import (
	"fmt"
	"strings"
	"time"

	"github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxlan-go/pkg/messages"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

const (
	discoverySettlingDelay  = 2 * time.Second
	matrixStateWaitTimeout  = 3 * time.Second
	matrixStatePollDelay    = 250 * time.Millisecond
	stateCaptureRestoreFade = 500 * time.Millisecond
)

type LifxDeviceController struct {
	controller *controller.Controller
	snapshots  []stateSnapshot
}

type stateSnapshot struct {
	serial       lifxdevice.Serial
	poweredOn    bool
	color        lifxdevice.Color
	zones        []packets.LightHsbk
	matrixChains [][]packets.LightHsbk
	matrixWidth  int
	matrix       bool
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

func (l *LifxDeviceController) SetZoneColors(target string, zones []ZoneColorParams, durationMS int64) error {
	if len(zones) == 0 {
		return nil
	}
	colors := make([]packets.LightHsbk, len(zones))
	for _, zone := range zones {
		if zone.Index < 0 || zone.Index >= len(colors) {
			continue
		}
		colors[zone.Index] = hsbk(zone.Hue, zone.Saturation, zone.Brightness, zone.Kelvin)
	}

	serials, err := l.resolveTarget(target)
	if err != nil {
		return err
	}
	for _, serial := range serials {
		for _, msg := range messages.SetMultizoneExtendedColors(0, colors, time.Duration(durationMS)*time.Millisecond) {
			if err := l.controller.Send(serial, msg); err != nil {
				return fmt.Errorf("send zone colors to %s: %w", serial, err)
			}
		}
	}
	return nil
}

func (l *LifxDeviceController) SetMatrixColors(target string, pixels []MatrixColorParams, width, height int, durationMS int64) error {
	if len(pixels) == 0 {
		return nil
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("matrix width and height are required")
	}
	colors := make([]packets.LightHsbk, width*height)
	for _, pixel := range pixels {
		if pixel.X < 0 || pixel.X >= width || pixel.Y < 0 || pixel.Y >= height {
			continue
		}
		colors[pixel.Y*width+pixel.X] = hsbk(pixel.Hue, pixel.Saturation, pixel.Brightness, pixel.Kelvin)
	}

	serials, err := l.resolveTarget(target)
	if err != nil {
		return err
	}
	for _, serial := range serials {
		length := l.matrixChainLength(serial)
		for _, msg := range messages.SetMatrixColorsFromSlice(0, length, width, colors, time.Duration(durationMS)*time.Millisecond) {
			if err := l.controller.Send(serial, msg); err != nil {
				return fmt.Errorf("send matrix colors to %s: %w", serial, err)
			}
		}
	}
	return nil
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

	devices, err := l.waitForRestorableState(serialSet, matrixStateWaitTimeout)
	if err != nil {
		return err
	}
	snapshots := make([]stateSnapshot, 0, len(serials))
	for _, device := range devices {
		if !serialSet[device.Serial] {
			continue
		}
		snapshots = append(snapshots, stateSnapshot{
			serial:       device.Serial,
			poweredOn:    device.PoweredOn,
			color:        device.Color,
			zones:        cloneHSBKs(device.MultizoneProperties.Zones),
			matrixChains: cloneMatrixChains(device.MatrixProperties.ChainZones),
			matrixWidth:  device.MatrixProperties.Width,
			matrix:       device.LightType == lifxdevice.LightTypeMatrix,
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
		if !snapshot.matrix {
			if err := l.SetColor(target, ColorParams{
				Hue:        snapshot.color.Hue,
				Saturation: snapshot.color.Saturation,
				Brightness: snapshot.color.Brightness,
				Kelvin:     int(snapshot.color.Kelvin),
				DurationMS: stateCaptureRestoreFade.Milliseconds(),
			}); err != nil && restoreErr == nil {
				restoreErr = err
			}
		}

		if len(snapshot.zones) > 0 {
			if err := l.restoreZoneColors(snapshot.serial, snapshot.zones, stateCaptureRestoreFade); err != nil && restoreErr == nil {
				restoreErr = err
			}
		}

		if len(snapshot.matrixChains) > 0 {
			if err := l.restoreMatrixColors(snapshot.serial, snapshot.matrixWidth, snapshot.matrixChains, stateCaptureRestoreFade); err != nil && restoreErr == nil {
				restoreErr = err
			}
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

func (l *LifxDeviceController) restoreZoneColors(serial lifxdevice.Serial, zones []packets.LightHsbk, duration time.Duration) error {
	for _, msg := range messages.SetMultizoneExtendedColors(0, zones, duration) {
		if err := l.controller.Send(serial, msg); err != nil {
			return fmt.Errorf("restore zone colors to %s: %w", serial, err)
		}
	}
	return nil
}

func (l *LifxDeviceController) restoreMatrixColors(serial lifxdevice.Serial, width int, chains [][]packets.LightHsbk, duration time.Duration) error {
	for chainIndex, colors := range chains {
		if len(colors) == 0 {
			continue
		}
		sendWidth := matrixRestoreWidth(width, len(colors))
		for _, msg := range messages.SetMatrixColorsFromSlice(chainIndex, 1, sendWidth, colors, duration) {
			if err := l.controller.Send(serial, msg); err != nil {
				return fmt.Errorf("restore matrix colors to %s chain %d: %w", serial, chainIndex, err)
			}
		}
	}
	return nil
}

func (l *LifxDeviceController) Devices() ([]DeviceInfo, error) {
	if l.controller == nil {
		return nil, fmt.Errorf("lifx controller is not initialized")
	}

	discovered := l.controller.GetDevices()
	infos := make([]DeviceInfo, 0, len(discovered))
	for _, device := range discovered {
		capabilities := deviceCapabilities(device)
		infos = append(infos, DeviceInfo{
			ID:           device.Serial.String(),
			Label:        device.Label,
			Group:        device.Group,
			Location:     device.Location,
			Capabilities: capabilities,
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

	for _, selector := range splitSelectors(key) {
		matches := resolveSelector(selector, devices)
		for _, serial := range matches {
			add(serial)
		}
	}

	if len(serials) == 0 {
		return nil, fmt.Errorf("no LIFX devices match target %q", target)
	}

	return serials, nil
}

func (l *LifxDeviceController) matrixChainLength(serial lifxdevice.Serial) int {
	if l.controller == nil {
		return 1
	}
	for _, device := range l.controller.GetDevices() {
		if device.Serial == serial {
			return matrixChainLength(device)
		}
	}
	return 1
}

func splitSelectors(target string) []string {
	parts := strings.Split(target, ",")
	selectors := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			selectors = append(selectors, part)
		}
	}
	return selectors
}

func resolveSelector(key string, devices []lifxdevice.Device) []lifxdevice.Serial {
	var serials []lifxdevice.Serial

	if key == "all" {
		for _, device := range devices {
			serials = append(serials, device.Serial)
		}
		return serials
	}

	if serial, err := lifxdevice.SerialFromHex(key); err == nil {
		for _, device := range devices {
			if device.Serial == serial {
				return []lifxdevice.Serial{serial}
			}
		}
	}

	for _, device := range devices {
		if strings.ToLower(device.Label) == key ||
			strings.ToLower(device.Group) == key ||
			strings.ToLower(device.Location) == key {
			serials = append(serials, device.Serial)
		}
	}

	return serials
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

func hsbk(hue, saturation, brightness float64, kelvin int) packets.LightHsbk {
	if kelvin <= 0 {
		kelvin = 3500
	}
	return packets.LightHsbk{
		Hue:        lifxdevice.ConvertExternalToDeviceValue(clamp(hue, 0, 360), 360),
		Saturation: lifxdevice.ConvertExternalToDeviceValue(normalizePercent(saturation), 100),
		Brightness: lifxdevice.ConvertExternalToDeviceValue(normalizePercent(brightness), 100),
		Kelvin:     uint16(kelvin),
	}
}

func deviceCapabilities(device lifxdevice.Device) DeviceCapabilities {
	surface := lifxdevice.SurfaceFromDevice(device)
	kind := deviceKindFromDevice(device)

	return DeviceCapabilities{
		Kind:         kind,
		HasColor:     device.ColorProperties.HasColor,
		HasKelvin:    device.ColorProperties.TemperatureRange.Min > 0 || device.ColorProperties.TemperatureRange.Max > 0,
		ZoneCount:    zoneCountFromDevice(kind, device, surface),
		MatrixWidth:  matrixWidthFromDevice(kind, device, surface),
		MatrixHeight: matrixHeightFromDevice(kind, device, surface),
		MatrixLength: matrixLengthFromSurface(surface, device.MatrixProperties.ChainLength),
		Surface:      surface,
	}
}

func deviceKindFromDevice(device lifxdevice.Device) DeviceKind {
	if device.Type == lifxdevice.DeviceTypeSwitch {
		return DeviceKindSwitch
	}
	return deviceKindFromLightType(device.LightType)
}

func deviceKindFromLightType(lightType lifxdevice.LightType) DeviceKind {
	switch lightType {
	case lifxdevice.LightTypeMultiZone:
		return DeviceKindMultiZone
	case lifxdevice.LightTypeMatrix:
		return DeviceKindMatrix
	default:
		return DeviceKindSingleZone
	}
}

func zoneCount(device lifxdevice.Device) int {
	return zoneCountFromDevice(
		deviceKindFromDevice(device),
		device,
		lifxdevice.SurfaceFromDevice(device),
	)
}

func zoneCountFromDevice(kind DeviceKind, device lifxdevice.Device, surface lifxdevice.Surface) int {
	switch kind {
	case DeviceKindMultiZone:
		if count := len(device.MultizoneProperties.Zones); count > 0 {
			return count
		}
		if surface.Zones > 0 {
			return surface.Zones
		}
	case DeviceKindMatrix:
		if device.MatrixProperties.NZones > 0 {
			return device.MatrixProperties.NZones
		}
		if device.MatrixProperties.Width > 0 && device.MatrixProperties.Height > 0 {
			return device.MatrixProperties.Width * device.MatrixProperties.Height
		}
		if surface.Zones > 0 {
			return surface.Zones
		}
		if surface.Width > 0 && surface.Height > 0 {
			return surface.Width * surface.Height
		}
	default:
		return 1
	}
	return 1
}

func matrixWidthFromDevice(kind DeviceKind, device lifxdevice.Device, surface lifxdevice.Surface) int {
	if kind == DeviceKindMatrix && device.MatrixProperties.Width > 0 {
		return device.MatrixProperties.Width
	}
	if kind == DeviceKindMatrix && surface.Width > 0 {
		return surface.Width
	}
	return 0
}

func matrixHeightFromDevice(kind DeviceKind, device lifxdevice.Device, surface lifxdevice.Surface) int {
	if kind == DeviceKindMatrix && device.MatrixProperties.Height > 0 {
		return device.MatrixProperties.Height
	}
	if kind == DeviceKindMatrix && surface.Height > 0 {
		return surface.Height
	}
	return 0
}

func matrixChainLength(device lifxdevice.Device) int {
	return matrixLengthFromSurface(lifxdevice.SurfaceFromDevice(device), device.MatrixProperties.ChainLength)
}

func matrixLengthFromSurface(surface lifxdevice.Surface, fallback int) int {
	if surface.Matrix != nil && len(surface.Matrix.Chains) > 0 {
		return len(surface.Matrix.Chains)
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

func (l *LifxDeviceController) waitForRestorableState(serials map[lifxdevice.Serial]bool, timeout time.Duration) ([]lifxdevice.Device, error) {
	deadline := time.Now().Add(timeout)
	for {
		devices := l.controller.GetDevices()
		if err := l.requestRestorableState(serials, devices); err != nil {
			return nil, err
		}
		if matrixStateReady(serials, devices) || time.Now().After(deadline) {
			return devices, nil
		}
		time.Sleep(matrixStatePollDelay)
	}
}

func (l *LifxDeviceController) requestRestorableState(serials map[lifxdevice.Serial]bool, devices []lifxdevice.Device) error {
	for _, device := range devices {
		if !serials[device.Serial] {
			continue
		}
		for _, msg := range restorableStateMessages(device) {
			if err := l.controller.Send(device.Serial, msg); err != nil {
				return fmt.Errorf("request state from %s: %w", device.Serial, err)
			}
		}
	}
	return nil
}

func restorableStateMessages(device lifxdevice.Device) []*protocol.Message {
	if device.LightType == lifxdevice.LightTypeMatrix && device.MatrixProperties.ChainLength == 0 {
		return []*protocol.Message{
			protocol.NewMessage(&packets.LightGet{}),
			protocol.NewMessage(&packets.DeviceGetPower{}),
			protocol.NewMessage(&packets.TileGetDeviceChain{}),
		}
	}

	return device.HighFreqStateMessages()
}

func matrixStateReady(serials map[lifxdevice.Serial]bool, devices []lifxdevice.Device) bool {
	for _, device := range devices {
		if !serials[device.Serial] || device.LightType != lifxdevice.LightTypeMatrix || !device.PoweredOn {
			continue
		}
		if !matrixDeviceStateReady(device) {
			return false
		}
	}
	return true
}

func matrixDeviceStateReady(device lifxdevice.Device) bool {
	length := matrixChainLength(device)
	if length <= 0 || len(device.MatrixProperties.ChainZones) < length {
		return false
	}
	for _, colors := range device.MatrixProperties.ChainZones[:length] {
		if !hasVisibleHSBK(colors) {
			return false
		}
	}
	return true
}

func cloneHSBKs(colors []packets.LightHsbk) []packets.LightHsbk {
	if !hasVisibleHSBK(colors) {
		return nil
	}
	return append([]packets.LightHsbk(nil), colors...)
}

func cloneMatrixChains(chains [][]packets.LightHsbk) [][]packets.LightHsbk {
	if len(chains) == 0 {
		return nil
	}
	cloned := make([][]packets.LightHsbk, len(chains))
	hasState := false
	for i, colors := range chains {
		if hasVisibleHSBK(colors) {
			cloned[i] = append([]packets.LightHsbk(nil), colors...)
			hasState = true
		}
	}
	if !hasState {
		return nil
	}
	return cloned
}

func hasVisibleHSBK(colors []packets.LightHsbk) bool {
	for _, color := range colors {
		if color.Hue != 0 || color.Saturation != 0 || color.Brightness != 0 || color.Kelvin != 0 {
			return true
		}
	}
	return false
}

func matrixRestoreWidth(width, colorCount int) int {
	if width > 0 {
		return width
	}
	if colorCount <= 0 {
		return 1
	}
	if colorCount%8 == 0 {
		return 8
	}
	return colorCount
}
