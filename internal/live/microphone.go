package live

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

const (
	DefaultCaptureSampleRate = 16_000
	DefaultCapturePeriod     = 20 * time.Millisecond
)

type InputDevice struct {
	ID      string
	Name    string
	Default bool
}

type MicrophoneConfig struct {
	Device         string
	SampleRate     int
	PeriodDuration time.Duration
}

type MicrophoneSource struct {
	config MicrophoneConfig

	mu   sync.RWMutex
	name string
}

func NewMicrophoneSource(config MicrophoneConfig) *MicrophoneSource {
	if config.SampleRate <= 0 {
		config.SampleRate = DefaultCaptureSampleRate
	}
	if config.PeriodDuration <= 0 {
		config.PeriodDuration = DefaultCapturePeriod
	}
	name := strings.TrimSpace(config.Device)
	if name == "" {
		name = "Default microphone"
	}
	return &MicrophoneSource{config: config, name: name}
}

func (s *MicrophoneSource) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

func (s *MicrophoneSource) Run(ctx context.Context, output chan<- PCMChunk) error {
	malgoContext, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return fmt.Errorf("initialize audio capture: %w", err)
	}
	defer func() {
		_ = malgoContext.Uninit()
		malgoContext.Free()
	}()

	infos, err := malgoContext.Devices(malgo.Capture)
	if err != nil {
		return fmt.Errorf("list capture devices: %w", err)
	}
	selected, err := selectCaptureDevice(infos, s.config.Device)
	if err != nil {
		return err
	}
	if selected != nil {
		s.mu.Lock()
		s.name = selected.Name()
		s.mu.Unlock()
	} else if defaultDevice := findDefaultCaptureDevice(infos); defaultDevice != nil {
		s.mu.Lock()
		s.name = defaultDevice.Name()
		s.mu.Unlock()
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatF32
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = uint32(s.config.SampleRate)
	deviceConfig.PeriodSizeInMilliseconds = uint32(max(s.config.PeriodDuration.Milliseconds(), 1))
	deviceConfig.PerformanceProfile = malgo.LowLatency
	deviceConfig.Alsa.NoMMap = 1
	if selected != nil {
		deviceConfig.Capture.DeviceID = unsafe.Pointer(&selected.ID)
	}

	started := time.Now()
	callbacks := malgo.DeviceCallbacks{Data: func(_, input []byte, _ uint32) {
		if len(input) < 4 {
			return
		}
		samples := make([]float32, len(input)/4)
		for index := range samples {
			bits := binary.LittleEndian.Uint32(input[index*4 : index*4+4])
			samples[index] = math.Float32frombits(bits)
		}
		chunk := PCMChunk{Samples: samples, SampleRate: s.config.SampleRate, CapturedAt: time.Since(started)}
		select {
		case output <- chunk:
		default:
		}
	}}

	device, err := malgo.InitDevice(malgoContext.Context, deviceConfig, callbacks)
	runtime.KeepAlive(selected)
	if err != nil {
		return fmt.Errorf("open microphone %q: %w", s.Name(), err)
	}
	defer device.Uninit()
	if err := device.Start(); err != nil {
		return fmt.Errorf("start microphone %q: %w", s.Name(), err)
	}

	<-ctx.Done()
	return nil
}

func ListInputDevices() ([]InputDevice, error) {
	malgoContext, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("initialize audio capture: %w", err)
	}
	defer func() {
		_ = malgoContext.Uninit()
		malgoContext.Free()
	}()
	infos, err := malgoContext.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("list capture devices: %w", err)
	}
	result := make([]InputDevice, 0, len(infos))
	for index := range infos {
		result = append(result, InputDevice{
			ID:      infos[index].ID.String(),
			Name:    infos[index].Name(),
			Default: infos[index].IsDefault != 0,
		})
	}
	return result, nil
}

func selectCaptureDevice(infos []malgo.DeviceInfo, selector string) (*malgo.DeviceInfo, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" || strings.EqualFold(selector, "default") {
		return nil, nil
	}
	for index := range infos {
		if strings.EqualFold(infos[index].ID.String(), selector) || strings.EqualFold(infos[index].Name(), selector) {
			return &infos[index], nil
		}
	}
	var match *malgo.DeviceInfo
	for index := range infos {
		if strings.Contains(strings.ToLower(infos[index].Name()), strings.ToLower(selector)) {
			if match != nil {
				return nil, fmt.Errorf("microphone %q is ambiguous; use its full name or ID", selector)
			}
			match = &infos[index]
		}
	}
	if match != nil {
		return match, nil
	}
	return nil, fmt.Errorf("microphone %q was not found", selector)
}

func findDefaultCaptureDevice(infos []malgo.DeviceInfo) *malgo.DeviceInfo {
	for index := range infos {
		if infos[index].IsDefault != 0 {
			return &infos[index]
		}
	}
	return nil
}
