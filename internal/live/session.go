package live

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/playback"
)

type SessionConfig struct {
	Controller       devices.DeviceController
	Devices          []devices.DeviceInfo
	Source           AudioSource
	Analyzer         Analyzer
	Tracker          *StateTracker
	Generator        *Generator
	Observer         Observer
	MasterBrightness float64
	OnDispatchError  DispatchErrorFunc
}

// Session owns one complete Live run, including device capture and restoration.
// It deliberately has no CLI or Wails dependencies so any application surface
// can use the same lifecycle.
type Session struct {
	engine     *Engine
	analyzer   Analyzer
	generator  *Generator
	player     *playback.Player
	dispatcher *Dispatcher
	controller devices.DeviceController
	devices    []devices.DeviceInfo
}

func NewSession(config SessionConfig) (*Session, error) {
	if config.Controller == nil {
		return nil, fmt.Errorf("live device controller is required")
	}
	if len(config.Devices) == 0 {
		return nil, fmt.Errorf("live session requires at least one device")
	}

	player := playback.NewPlayer(config.Controller, playback.Options{MasterBrightness: config.MasterBrightness})
	dispatcher := NewDispatcher(player, config.OnDispatchError)
	engine, err := NewEngine(EngineConfig{
		Source: config.Source, Analyzer: config.Analyzer, Tracker: config.Tracker,
		Generator: config.Generator, Sink: dispatcher, Observer: config.Observer,
	})
	if err != nil {
		return nil, err
	}
	return &Session{
		engine: engine, analyzer: config.Analyzer, generator: config.Generator, player: player, dispatcher: dispatcher, controller: config.Controller,
		devices: append([]devices.DeviceInfo(nil), config.Devices...),
	}, nil
}

func (s *Session) Run(ctx context.Context) error {
	defer s.analyzer.Close()
	restore, err := captureSessionState(s.controller, sessionDeviceIDs(s.devices))
	if err != nil {
		return err
	}
	defer restore()

	if err := powerOnSessionDevices(s.controller, s.devices); err != nil {
		return err
	}
	return s.engine.Run(ctx)
}

func (s *Session) SetMasterBrightness(scale float64) {
	s.player.SetMasterBrightness(scale)
}

func (s *Session) SetStyle(style string) error {
	return s.generator.SetStyle(style)
}

// SetObserver configures diagnostics before Run starts.
func (s *Session) SetObserver(observer Observer) {
	s.engine.config.Observer = observer
}

func (s *Session) Dispatcher() *Dispatcher {
	return s.dispatcher
}

func captureSessionState(controller devices.DeviceController, target string) (func(), error) {
	restorer, ok := controller.(devices.StateRestorer)
	if !ok {
		return func() {}, nil
	}
	if err := restorer.CaptureState(target); err != nil {
		return nil, fmt.Errorf("capture live device state: %w", err)
	}
	return func() { _ = restorer.RestoreState() }, nil
}

func powerOnSessionDevices(controller devices.DeviceController, infos []devices.DeviceInfo) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(infos))
	for _, info := range infos {
		info := info
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := controller.PowerOn(info.ID); err != nil {
				errs <- fmt.Errorf("power on %s: %w", sessionDeviceName(info), err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return nil
}

func sessionDeviceIDs(infos []devices.DeviceInfo) string {
	ids := make([]string, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ID)
	}
	return strings.Join(ids, ",")
}

func sessionDeviceName(info devices.DeviceInfo) string {
	if info.Label != "" {
		return info.Label
	}
	return info.ID
}

var _ ImmediateExecutor = (*playback.Player)(nil)
