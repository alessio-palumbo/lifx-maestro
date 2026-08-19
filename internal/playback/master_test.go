package playback

import (
	"sync"
	"testing"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/scheduler"
	"lifx-maestro/internal/timeline"
)

type recordingController struct {
	mu      sync.Mutex
	colors  []devices.ColorParams
	zones   [][]devices.ZoneColorParams
	pixels  [][]devices.MatrixColorParams
	powered []string
}

func (c *recordingController) PowerOn(target string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.powered = append(c.powered, target)
	return nil
}

func (c *recordingController) PowerOff(string) error { return nil }

func (c *recordingController) SetColor(_ string, params devices.ColorParams) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.colors = append(c.colors, params)
	return nil
}

func (c *recordingController) SetZoneColors(_ string, zones []devices.ZoneColorParams, _ int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.zones = append(c.zones, zones)
	return nil
}

func (c *recordingController) SetMatrixColors(_ string, pixels []devices.MatrixColorParams, _, _ int, _ int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pixels = append(c.pixels, pixels)
	return nil
}

func colorEvent(brightness float64) scheduler.Event {
	return scheduler.Event{Value: timeline.Event{
		Target: "all",
		Action: "set_color",
		Params: timeline.MustParams(timeline.SetColorParams{Brightness: &brightness}),
	}}
}

func TestMasterBrightnessDefaultsToFull(t *testing.T) {
	controller := &recordingController{}
	player := NewPlayer(controller, Options{})

	if got := player.MasterBrightness(); got != 1 {
		t.Fatalf("master brightness = %v, want 1", got)
	}
	if err := player.execute(colorEvent(80)); err != nil {
		t.Fatal(err)
	}
	if got := controller.colors[0].Brightness; got != 80 {
		t.Fatalf("brightness = %v, want the event's own 80", got)
	}
}

func TestMasterBrightnessScalesDispatchedColours(t *testing.T) {
	controller := &recordingController{}
	player := NewPlayer(controller, Options{MasterBrightness: 0.5})

	if err := player.execute(colorEvent(80)); err != nil {
		t.Fatal(err)
	}
	if got := controller.colors[0].Brightness; got != 40 {
		t.Fatalf("brightness = %v, want 40", got)
	}
}

// The point of a fader is that it moves during a song, so a change has to reach
// events dispatched after it without restarting playback.
func TestMasterBrightnessAppliesToLaterEvents(t *testing.T) {
	controller := &recordingController{}
	player := NewPlayer(controller, Options{})

	if err := player.execute(colorEvent(60)); err != nil {
		t.Fatal(err)
	}
	player.SetMasterBrightness(0.25)
	if err := player.execute(colorEvent(60)); err != nil {
		t.Fatal(err)
	}

	if got := controller.colors[0].Brightness; got != 60 {
		t.Fatalf("first event = %v, want the unscaled 60", got)
	}
	if got := controller.colors[1].Brightness; got != 15 {
		t.Fatalf("second event = %v, want 15 after the fader moved", got)
	}
}

func TestMasterBrightnessScalesZonesAndPixels(t *testing.T) {
	controller := &recordingController{}
	player := NewPlayer(controller, Options{MasterBrightness: 0.5})

	zones := scheduler.Event{Value: timeline.Event{
		Target: "strip",
		Action: "set_zone_colors",
		Params: timeline.MustParams(timeline.SetZoneColorsParams{
			DurationMS: 100,
			Zones: []timeline.ZoneColorParams{
				{Index: 0, Color: timeline.ColorValue{Brightness: 90}},
				{Index: 1, Color: timeline.ColorValue{Brightness: 40}},
			},
		}),
	}}
	pixels := scheduler.Event{Value: timeline.Event{
		Target: "tile",
		Action: "set_matrix_colors",
		Params: timeline.MustParams(timeline.SetMatrixColorsParams{
			Width: 1, Height: 2, DurationMS: 100,
			Pixels: []timeline.MatrixColorParams{
				{X: 0, Y: 0, Color: timeline.ColorValue{Brightness: 90}},
				{X: 0, Y: 1, Color: timeline.ColorValue{Brightness: 40}},
			},
		}),
	}}

	if err := player.execute(zones); err != nil {
		t.Fatal(err)
	}
	if err := player.execute(pixels); err != nil {
		t.Fatal(err)
	}

	if got := controller.zones[0][0].Brightness; got != 45 {
		t.Fatalf("zone 0 brightness = %v, want 45", got)
	}
	if got := controller.zones[0][1].Brightness; got != 20 {
		t.Fatalf("zone 1 brightness = %v, want 20", got)
	}
	if got := controller.pixels[0][0].Brightness; got != 45 {
		t.Fatalf("pixel 0 brightness = %v, want 45", got)
	}
	if got := controller.pixels[0][1].Brightness; got != 20 {
		t.Fatalf("pixel 1 brightness = %v, want 20", got)
	}
}

// Relative dynamics are the whole reason for scaling rather than capping: a dimmed
// show must keep the difference between its loud and quiet moments.
func TestMasterBrightnessPreservesRelativeDynamics(t *testing.T) {
	controller := &recordingController{}
	player := NewPlayer(controller, Options{MasterBrightness: 0.4})

	for _, brightness := range []float64{100, 50, 25} {
		if err := player.execute(colorEvent(brightness)); err != nil {
			t.Fatal(err)
		}
	}

	first, second, third := controller.colors[0].Brightness, controller.colors[1].Brightness, controller.colors[2].Brightness
	if first/second < 1.9 || first/second > 2.1 {
		t.Fatalf("ratio between 100 and 50 became %.2f, want about 2", first/second)
	}
	if second/third < 1.9 || second/third > 2.1 {
		t.Fatalf("ratio between 50 and 25 became %.2f, want about 2", second/third)
	}
}

// A fader at zero must not send a zero the device reads as "off/unset"; the
// existing floor keeps it at the minimum instead.
func TestMasterBrightnessAtZeroKeepsTheFloor(t *testing.T) {
	controller := &recordingController{}
	player := NewPlayer(controller, Options{})
	player.SetMasterBrightness(0)

	if err := player.execute(colorEvent(80)); err != nil {
		t.Fatal(err)
	}
	if got := controller.colors[0].Brightness; got != 1 {
		t.Fatalf("brightness = %v, want the 1%% floor", got)
	}
}

func TestMasterBrightnessClampsOutOfRangeValues(t *testing.T) {
	player := NewPlayer(&recordingController{}, Options{})

	player.SetMasterBrightness(4)
	if got := player.MasterBrightness(); got != 1 {
		t.Fatalf("above range = %v, want 1", got)
	}
	player.SetMasterBrightness(-2)
	if got := player.MasterBrightness(); got != 0 {
		t.Fatalf("below range = %v, want 0", got)
	}
}
