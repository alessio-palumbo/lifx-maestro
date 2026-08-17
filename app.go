package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	"lifx-maestro/internal/playback"
	"lifx-maestro/internal/timeline"
)

type App struct {
	ctx          context.Context
	previewMu    sync.Mutex
	previewStop  context.CancelFunc
	previewAudio *audio.BeepPlayer
	previewLifx  *devices.LifxDeviceController
}

type EditorSession struct {
	SongPath   string                `json:"song_path"`
	SongName   string                `json:"song_name"`
	Style      string                `json:"style"`
	Target     string                `json:"target"`
	Analysis   analysis.SongAnalysis `json:"analysis"`
	Timeline   EditorTimeline        `json:"timeline"`
	Devices    []EditorDevice        `json:"devices"`
	Summary    EditorSummary         `json:"summary"`
	Generated  string                `json:"generated"`
	Source     string                `json:"source"`
	EventStats map[string]int        `json:"event_stats"`
}

type EditorSummary struct {
	BPM        float64 `json:"bpm"`
	DurationMS int64   `json:"duration_ms"`
	Beats      int     `json:"beats"`
	Sections   int     `json:"sections"`
	Events     int     `json:"events"`
}

type SaveTimelineRequest struct {
	Path     string         `json:"path"`
	Timeline EditorTimeline `json:"timeline"`
}

type PreviewRequest struct {
	AudioPath string         `json:"audio_path"`
	Target    string         `json:"target"`
	Timeline  EditorTimeline `json:"timeline"`
}

type EditorTimeline struct {
	Name       string        `json:"name"`
	DurationMS int64         `json:"duration_ms"`
	Events     []EditorEvent `json:"events"`
}

type EditorEvent struct {
	TimeMS int64          `json:"time_ms"`
	Target string         `json:"target"`
	Action string         `json:"action"`
	Params map[string]any `json:"params,omitempty"`
}

type EditorDevice struct {
	ID           string                   `json:"id"`
	Label        string                   `json:"label"`
	Group        string                   `json:"group"`
	Location     string                   `json:"location"`
	Capabilities EditorDeviceCapabilities `json:"capabilities"`
}

type EditorDeviceCapabilities struct {
	Kind         devices.DeviceKind `json:"kind"`
	ZoneCount    int                `json:"zone_count"`
	MatrixWidth  int                `json:"matrix_width"`
	MatrixHeight int                `json:"matrix_height"`
	MatrixLength int                `json:"matrix_length"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) Styles() []string {
	return generation.AvailableStyles()
}

func (a *App) DiscoverDevices() ([]EditorDevice, error) {
	controller, err := devices.NewLifxDeviceController()
	if err != nil {
		return nil, err
	}
	defer controller.Close()

	infos, err := controller.Devices()
	if err != nil {
		return nil, err
	}
	return editorDevicesFromInfos(infos), nil
}

func (a *App) ChooseAudioFile() (string, error) {
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Choose song",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Audio files", Pattern: "*.mp3;*.wav"},
		},
	})
}

func (a *App) AudioDuration(audioPath string) (int64, error) {
	player, err := audio.NewBeepPlayer(audioPath)
	if err != nil {
		return 0, err
	}
	defer player.Stop()
	return player.Duration().Milliseconds(), nil
}

func (a *App) ChooseTimelineSavePath(defaultName string) (string, error) {
	if strings.TrimSpace(defaultName) == "" {
		defaultName = "timeline.json"
	}
	return wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save timeline JSON",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Timeline JSON", Pattern: "*.json"},
		},
	})
}

func (a *App) Generate(audioPath string, style string, target string, editorDevices []EditorDevice) (*EditorSession, error) {
	if strings.TrimSpace(audioPath) == "" {
		return nil, fmt.Errorf("audio path is required")
	}
	if err := audio.ValidateInput(audioPath); err != nil {
		return nil, err
	}
	if style == "" {
		style = "synthwave"
	}
	if target == "" {
		target = "all"
	}

	analyzer := analysis.NewAnalyzer()
	analyzer.PythonPath = defaultUIPythonPath()
	result, err := analyzer.Analyze(a.ctx, audioPath)
	if err != nil {
		return nil, err
	}

	return buildEditorSessionWithDevices(audioPath, filepath.Base(audioPath), style, target, "generated", *result, editorDevices)
}

func (a *App) SaveTimeline(request SaveTimelineRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return fmt.Errorf("path is required")
	}
	tl, err := timelineFromEditor(request.Timeline)
	if err != nil {
		return err
	}
	return timeline.Save(request.Path, tl)
}

func (a *App) StartPreview(request PreviewRequest) error {
	if strings.TrimSpace(request.AudioPath) == "" {
		return fmt.Errorf("audio path is required")
	}
	if len(request.Timeline.Events) == 0 {
		return fmt.Errorf("timeline has no lighting events")
	}
	if request.Target == "" {
		request.Target = "all"
	}

	tl, err := timelineFromEditor(request.Timeline)
	if err != nil {
		return err
	}

	a.StopPreview()

	controller, err := devices.NewLifxDeviceController()
	if err != nil {
		return err
	}

	restore := setupWailsStateRestore(controller, request.Target)
	audioPlayer, err := audio.NewBeepPlayer(request.AudioPath)
	if err != nil {
		restore()
		controller.Close()
		return err
	}

	ctx, cancel := context.WithCancel(a.ctx)
	lightingPlayer := playback.NewPlayer(controller, playback.Options{ClockLabel: "audio"})

	a.previewMu.Lock()
	a.previewStop = cancel
	a.previewAudio = audioPlayer
	a.previewLifx = controller
	a.previewMu.Unlock()

	go func() {
		defer restore()
		defer controller.Close()
		defer audioPlayer.Stop()
		defer a.clearPreview(controller)
		_ = lightingPlayer.PlayWithClock(ctx, tl, audioPlayer)
		select {
		case <-audioPlayer.Done():
		case <-ctx.Done():
		}
	}()

	if err := audioPlayer.Play(); err != nil {
		cancel()
		return err
	}
	return nil
}

func (a *App) StartAudioPreview(audioPath string) error {
	if strings.TrimSpace(audioPath) == "" {
		return fmt.Errorf("audio path is required")
	}

	a.StopPreview()

	audioPlayer, err := audio.NewBeepPlayer(audioPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(a.ctx)

	a.previewMu.Lock()
	a.previewStop = cancel
	a.previewAudio = audioPlayer
	a.previewLifx = nil
	a.previewMu.Unlock()

	go func() {
		defer audioPlayer.Stop()
		defer a.clearPreview(nil)
		select {
		case <-audioPlayer.Done():
		case <-ctx.Done():
		}
	}()

	if err := audioPlayer.Play(); err != nil {
		cancel()
		return err
	}
	return nil
}

func (a *App) StopPreview() {
	a.previewMu.Lock()
	cancel := a.previewStop
	audioPlayer := a.previewAudio
	a.previewStop = nil
	a.previewAudio = nil
	a.previewLifx = nil
	a.previewMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if audioPlayer != nil {
		_ = audioPlayer.Stop()
	}
}

func (a *App) PausePreview() error {
	a.previewMu.Lock()
	audioPlayer := a.previewAudio
	a.previewMu.Unlock()

	if audioPlayer == nil {
		return nil
	}
	return audioPlayer.Pause()
}

func (a *App) ResumePreview() error {
	a.previewMu.Lock()
	audioPlayer := a.previewAudio
	a.previewMu.Unlock()

	if audioPlayer == nil {
		return fmt.Errorf("preview is not active")
	}
	return audioPlayer.Play()
}

func (a *App) clearPreview(controller *devices.LifxDeviceController) {
	a.previewMu.Lock()
	defer a.previewMu.Unlock()
	if a.previewLifx == controller {
		a.previewStop = nil
		a.previewAudio = nil
		a.previewLifx = nil
	}
}

func buildEditorSession(songPath string, songName string, style string, target string, source string, song analysis.SongAnalysis) (*EditorSession, error) {
	return buildEditorSessionWithDevices(songPath, songName, style, target, source, song, nil)
}

func buildEditorSessionWithDevices(songPath string, songName string, style string, target string, source string, song analysis.SongAnalysis, editorDevices []EditorDevice) (*EditorSession, error) {
	infos := editorDeviceInfosFromEditor(editorDevices)
	if len(infos) == 0 {
		infos = editorDeviceInfos()
	}
	tl, err := generation.Generate(song, generation.Options{
		Name:    songName,
		Target:  target,
		Style:   style,
		Devices: infos,
	})
	if err != nil {
		return nil, err
	}

	return &EditorSession{
		SongPath:   songPath,
		SongName:   songName,
		Style:      style,
		Target:     target,
		Analysis:   song,
		Timeline:   editorTimelineFromTimeline(*tl),
		Devices:    editorDevicesFromInfos(infos),
		Summary:    summarize(song, editorTimelineFromTimeline(*tl)),
		Generated:  time.Now().Format(time.RFC3339),
		Source:     source,
		EventStats: eventStats(*tl),
	}, nil
}

func summarize(song analysis.SongAnalysis, tl EditorTimeline) EditorSummary {
	return EditorSummary{
		BPM:        song.BPM,
		DurationMS: song.DurationMS,
		Beats:      len(song.Beats),
		Sections:   len(song.Sections),
		Events:     len(tl.Events),
	}
}

func eventStats(tl timeline.Timeline) map[string]int {
	stats := make(map[string]int)
	for _, event := range tl.Events {
		stats[event.Action]++
	}
	return stats
}

func editorTimelineFromTimeline(tl timeline.Timeline) EditorTimeline {
	events := make([]EditorEvent, 0, len(tl.Events))
	for _, event := range tl.Events {
		events = append(events, EditorEvent{
			TimeMS: event.TimeMS,
			Target: event.Target,
			Action: event.Action,
			Params: paramsFromRaw(event.Params),
		})
	}
	return EditorTimeline{
		Name:       tl.Name,
		DurationMS: tl.DurationMS,
		Events:     events,
	}
}

func timelineFromEditor(tl EditorTimeline) (*timeline.Timeline, error) {
	events := make([]timeline.Event, 0, len(tl.Events))
	for _, event := range tl.Events {
		params, err := timeline.MarshalParams(event.Params)
		if err != nil {
			return nil, err
		}
		events = append(events, timeline.Event{
			TimeMS: event.TimeMS,
			Target: event.Target,
			Action: event.Action,
			Params: params,
		})
	}
	return &timeline.Timeline{
		Name:       tl.Name,
		DurationMS: tl.DurationMS,
		Events:     events,
	}, nil
}

func paramsFromRaw(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil
	}
	return params
}

func setupWailsStateRestore(controller devices.DeviceController, target string) func() {
	restorer, ok := controller.(devices.StateRestorer)
	if !ok {
		return func() {}
	}
	if err := restorer.CaptureState(target); err != nil {
		return func() {}
	}
	return func() {
		_ = restorer.RestoreState()
	}
}

func editorDevicesFromInfos(infos []devices.DeviceInfo) []EditorDevice {
	result := make([]EditorDevice, 0, len(infos))
	for _, info := range infos {
		result = append(result, EditorDevice{
			ID:       info.ID,
			Label:    info.Label,
			Group:    info.Group,
			Location: info.Location,
			Capabilities: EditorDeviceCapabilities{
				Kind:         info.Capabilities.Kind,
				ZoneCount:    info.Capabilities.ZoneCount,
				MatrixWidth:  info.Capabilities.MatrixWidth,
				MatrixHeight: info.Capabilities.MatrixHeight,
				MatrixLength: info.Capabilities.MatrixLength,
			},
		})
	}
	return result
}

func editorDeviceInfosFromEditor(editorDevices []EditorDevice) []devices.DeviceInfo {
	infos := make([]devices.DeviceInfo, 0, len(editorDevices))
	for _, device := range editorDevices {
		if device.ID == "" {
			continue
		}
		infos = append(infos, devices.DeviceInfo{
			ID:       device.ID,
			Label:    device.Label,
			Group:    device.Group,
			Location: device.Location,
			Capabilities: devices.DeviceCapabilities{
				Kind:         device.Capabilities.Kind,
				ZoneCount:    device.Capabilities.ZoneCount,
				MatrixWidth:  device.Capabilities.MatrixWidth,
				MatrixHeight: device.Capabilities.MatrixHeight,
				MatrixLength: device.Capabilities.MatrixLength,
				HasColor:     true,
				HasKelvin:    true,
			},
		})
	}
	return infos
}

func editorDeviceInfos() []devices.DeviceInfo {
	return []devices.DeviceInfo{
		{
			ID:       "desk",
			Label:    "Desk Lamp",
			Group:    "Office",
			Location: "Studio",
			Capabilities: devices.DeviceCapabilities{
				Kind:      devices.DeviceKindSingleZone,
				HasColor:  true,
				HasKelvin: true,
				ZoneCount: 1,
			},
		},
		{
			ID:       "tv",
			Label:    "TV Strip",
			Group:    "Lounge",
			Location: "Studio",
			Capabilities: devices.DeviceCapabilities{
				Kind:      devices.DeviceKindMultiZone,
				HasColor:  true,
				HasKelvin: true,
				ZoneCount: 32,
			},
		},
		{
			ID:       "tile",
			Label:    "Tile Matrix",
			Group:    "Wall",
			Location: "Studio",
			Capabilities: devices.DeviceCapabilities{
				Kind:         devices.DeviceKindMatrix,
				HasColor:     true,
				HasKelvin:    true,
				ZoneCount:    128,
				MatrixWidth:  8,
				MatrixHeight: 8,
				MatrixLength: 2,
			},
		},
		{
			ID:       "floor",
			Label:    "Floor Lamp",
			Group:    "Lounge",
			Location: "Studio",
			Capabilities: devices.DeviceCapabilities{
				Kind:      devices.DeviceKindSingleZone,
				HasColor:  true,
				HasKelvin: true,
				ZoneCount: 1,
			},
		},
	}
}

func defaultUIPythonPath() string {
	candidates := []string{
		filepath.Join("analyzer", ".venv", "bin", "python"),
		filepath.Join(".venv", "bin", "python"),
	}
	if stdruntime.GOOS == "windows" {
		candidates = []string{
			filepath.Join("analyzer", ".venv", "Scripts", "python.exe"),
			filepath.Join(".venv", "Scripts", "python.exe"),
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if stdruntime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func prettyParams(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(data)
}
