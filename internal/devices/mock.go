package devices

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type MockDeviceController struct {
	out   io.Writer
	start time.Time
	mu    sync.Mutex
}

func NewMockDeviceController(out io.Writer) *MockDeviceController {
	return &MockDeviceController{
		out:   out,
		start: time.Now(),
	}
}

func (m *MockDeviceController) PowerOn(target string) error {
	m.print(target, "power_on")
	return nil
}

func (m *MockDeviceController) PowerOff(target string) error {
	m.print(target, "power_off")
	return nil
}

func (m *MockDeviceController) SetColor(target string, params ColorParams) error {
	m.print(target, fmt.Sprintf(
		"set_color(hue=%.1f saturation=%.3f brightness=%.3f kelvin=%d duration_ms=%d)",
		params.Hue,
		params.Saturation,
		params.Brightness,
		params.Kelvin,
		params.DurationMS,
	))
	return nil
}

func (m *MockDeviceController) print(target, action string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Fprintf(m.out, "[%s] %s -> %s\n", formatOffset(time.Since(m.start)), target, action)
}

func formatOffset(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	totalMS := d.Milliseconds()
	minutes := totalMS / 60000
	seconds := (totalMS / 1000) % 60
	millis := totalMS % 1000
	return fmt.Sprintf("%02d:%02d.%03d", minutes, seconds, millis)
}
