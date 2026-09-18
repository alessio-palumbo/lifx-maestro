package live

import (
	"context"
	"errors"
	"sync"
	"testing"

	"lifx-maestro/internal/devices"
)

type sessionController struct {
	mu         sync.Mutex
	captured   string
	powered    map[string]bool
	restored   bool
	captureErr error
}

func (c *sessionController) CaptureState(target string) error {
	c.captured = target
	return c.captureErr
}

func (c *sessionController) RestoreState() error {
	c.restored = true
	return nil
}

func (c *sessionController) PowerOn(target string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.powered == nil {
		c.powered = make(map[string]bool)
	}
	c.powered[target] = true
	return nil
}

func (c *sessionController) PowerOff(string) error                                        { return nil }
func (c *sessionController) SetColor(string, devices.ColorParams) error                   { return nil }
func (c *sessionController) SetZoneColors(string, []devices.ZoneColorParams, int64) error { return nil }
func (c *sessionController) SetMatrixColors(string, []devices.MatrixColorParams, int, int, int64) error {
	return nil
}

func TestSessionDeviceLifecycleCapturesPowersAndRestores(t *testing.T) {
	controller := &sessionController{}
	infos := []devices.DeviceInfo{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}}
	restore, err := captureSessionState(controller, sessionDeviceIDs(infos))
	if err != nil {
		t.Fatal(err)
	}
	if err := powerOnSessionDevices(controller, infos); err != nil {
		t.Fatal(err)
	}
	restore()

	if controller.captured != "one,two" {
		t.Fatalf("captured %q, want one,two", controller.captured)
	}
	if !controller.powered["one"] || !controller.powered["two"] {
		t.Fatalf("powered devices = %#v", controller.powered)
	}
	if !controller.restored {
		t.Fatal("device state was not restored")
	}
}

func TestNewSessionRequiresDevices(t *testing.T) {
	_, err := NewSession(SessionConfig{Controller: &sessionController{}})
	if err == nil {
		t.Fatal("session without devices was accepted")
	}
}

type closingAnalyzer struct {
	closed bool
}

func (a *closingAnalyzer) Analyze(context.Context, PCMWindow) (Features, error) {
	return Features{}, nil
}

func (a *closingAnalyzer) Close() error {
	a.closed = true
	return nil
}

type sessionWaitingSource struct{}

func (sessionWaitingSource) Name() string { return "test" }
func (sessionWaitingSource) Run(ctx context.Context, _ chan<- PCMChunk) error {
	<-ctx.Done()
	return nil
}

func TestSessionClosesAnalyzerWhenStateCaptureFails(t *testing.T) {
	controller := &sessionController{captureErr: errors.New("capture failed")}
	analyzer := &closingAnalyzer{}
	infos := []devices.DeviceInfo{{ID: "one", Capabilities: devices.DeviceCapabilities{Kind: devices.DeviceKindSingleZone, HasColor: true}}}
	generator, err := NewGenerator(GeneratorConfig{Style: "synthwave", Devices: infos})
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(SessionConfig{
		Controller: controller, Devices: infos, Source: sessionWaitingSource{}, Analyzer: analyzer, Generator: generator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Run(context.Background()); err == nil {
		t.Fatal("capture failure was not returned")
	}
	if !analyzer.closed {
		t.Fatal("analyzer was left running after capture failure")
	}
}
