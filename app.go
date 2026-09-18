package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/analyzerbin"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	livemode "lifx-maestro/internal/live"
	"lifx-maestro/internal/playback"
	"lifx-maestro/internal/timeline"
)

type App struct {
	ctx          context.Context
	previewMu    sync.Mutex
	previewStop  context.CancelFunc
	previewDone  chan struct{}
	previewAudio *audio.BeepPlayer
	previewLifx  *devices.LifxDeviceController
	previewLight *playback.Player
	liveMu       sync.Mutex
	liveStop     context.CancelFunc
	liveDone     chan struct{}
	liveSession  *livemode.Session
	lifxMu       sync.Mutex
	lifx         *devices.LifxDeviceController
	// analyzerMu prevents startup warmup and user-triggered analysis from
	// running the bundled analyzer at the same time on first launch.
	analyzerMu         sync.Mutex
	analyzerWarmupMu   sync.Mutex
	analyzerWarmupStop context.CancelFunc
	analyzerWarmupDone chan struct{}
	// masterBrightness survives between previews so the setting sticks, and is
	// pushed to the running player so the fader takes effect mid-song.
	masterBrightness atomic.Uint64
}

// masterBrightnessScale reports the fader position as a 0-1 factor, defaulting to
// full for a freshly started app.
func (a *App) masterBrightnessScale() float64 {
	bits := a.masterBrightness.Load()
	if bits == 0 {
		return 1
	}
	return math.Float64frombits(bits)
}

// SetMasterBrightness scales the output of the running show, as a percentage. The
// timeline is untouched, so its dynamics survive and nothing is regenerated.
func (a *App) SetMasterBrightness(percent float64) {
	scale := percent / 100
	if scale < 0 {
		scale = 0
	}
	if scale > 1 {
		scale = 1
	}
	a.masterBrightness.Store(math.Float64bits(scale))

	a.previewMu.Lock()
	player := a.previewLight
	a.previewMu.Unlock()
	if player != nil {
		player.SetMasterBrightness(scale)
	}
	a.liveMu.Lock()
	liveSession := a.liveSession
	a.liveMu.Unlock()
	if liveSession != nil {
		liveSession.SetMasterBrightness(scale)
	}
}

// MasterBrightness reports the fader position as a percentage, so the UI can show
// the stored value rather than assuming one.
func (a *App) MasterBrightness() float64 {
	return a.masterBrightnessScale() * 100
}

type EditorSession struct {
	SongPath   string                `json:"song_path"`
	SongName   string                `json:"song_name"`
	Style      string                `json:"style"`
	Generation string                `json:"generation"`
	Dynamics   string                `json:"dynamics"`
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
	HasColor     bool               `json:"has_color"`
	HasKelvin    bool               `json:"has_kelvin"`
	ZoneCount    int                `json:"zone_count"`
	MatrixWidth  int                `json:"matrix_width"`
	MatrixHeight int                `json:"matrix_height"`
	MatrixLength int                `json:"matrix_length"`
}

type LiveRequest struct {
	Input       string `json:"input"`
	Target      string `json:"target"`
	Style       string `json:"style"`
	Intensity   string `json:"intensity"`
	Sensitivity string `json:"sensitivity"`
}

type LiveInput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

type LiveState struct {
	ElapsedMS       int64   `json:"elapsed_ms"`
	Input           string  `json:"input"`
	InputDB         float64 `json:"input_db"`
	NoiseFloorDB    float64 `json:"noise_floor_db"`
	MarginDB        float64 `json:"margin_db"`
	Energy          float64 `json:"energy"`
	Presence        float64 `json:"presence"`
	Low             float64 `json:"low"`
	Mid             float64 `json:"mid"`
	High            float64 `json:"high"`
	TempoBPM        float64 `json:"tempo_bpm"`
	TempoConfidence float64 `json:"tempo_confidence"`
	Activity        float64 `json:"activity"`
	Intensity       float64 `json:"intensity"`
	Dynamics        string  `json:"dynamics"`
	Novelty         float64 `json:"novelty"`
	Active          bool    `json:"active"`
	Sustained       bool    `json:"sustained"`
	Onset           bool    `json:"onset"`
	Beat            bool    `json:"beat"`
	SectionChange   bool    `json:"section_change"`
}

type LiveStopped struct {
	Error string `json:"error,omitempty"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go func() {
		_, _ = a.lifxController()
	}()
	// Unpack and run the bundled analyzer up front so the user's first analysis
	// does not pay for either. Extraction takes about a second; the first run
	// costs far more, because the OS verifies every library in the bundle and the
	// analyzer compiles its hot paths. Analyze reports any failure when it
	// retries, so failures here are silent by design.
	a.startAnalyzerWarmup()
}

func (a *App) shutdown(ctx context.Context) {
	a.StopLive()
	a.StopPreview()
	a.cancelAnalyzerWarmup()

	a.lifxMu.Lock()
	controller := a.lifx
	a.lifx = nil
	a.lifxMu.Unlock()

	if controller != nil {
		_ = controller.Close()
	}
}

func (a *App) LiveInputs() ([]LiveInput, error) {
	inputs, err := livemode.ListInputDevices()
	if err != nil {
		return nil, err
	}
	result := make([]LiveInput, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, LiveInput{ID: input.ID, Name: input.Name, Default: input.Default})
	}
	return result, nil
}

func (a *App) StartLive(request LiveRequest) error {
	if request.Target == "" {
		request.Target = "all"
	}
	if request.Style == "" {
		request.Style = "synthwave"
	}
	if request.Intensity == "" {
		request.Intensity = string(generation.DynamicsAuto)
	}
	if request.Sensitivity == "" {
		request.Sensitivity = string(livemode.SensitivityLow)
	}
	trackerConfig, err := livemode.TrackerConfigForSensitivity(request.Sensitivity)
	if err != nil {
		return err
	}

	a.StopPreview()
	a.StopLive()
	a.waitForAnalyzerWarmup()
	if _, err := a.ensureAnalyzerInstalled(); err != nil && !errors.Is(err, analyzerbin.ErrNotBundled) {
		return fmt.Errorf("prepare analyzer: %w", err)
	}
	analyzerConfig, err := analyzerbin.NewAnalyzer()
	if err != nil {
		return fmt.Errorf("prepare analyzer: %w", err)
	}

	controller, err := a.lifxController()
	if err != nil {
		return err
	}
	infos, err := controller.Devices()
	if err != nil {
		return err
	}
	selected, err := devices.SelectDeviceInfos(infos, request.Target)
	if err != nil {
		return err
	}
	selected = devices.ControllableLights(selected)
	if len(selected) == 0 {
		return fmt.Errorf("target %q contains no controllable lights", request.Target)
	}

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	analyzer, err := livemode.NewPythonAnalyzer(ctx, analyzerConfig)
	if err != nil {
		cancel()
		return err
	}
	generator, err := livemode.NewGenerator(livemode.GeneratorConfig{
		Style: request.Style, Intensity: generation.DynamicsOverride(request.Intensity), Devices: selected,
	})
	if err != nil {
		cancel()
		_ = analyzer.Close()
		return err
	}
	source := livemode.NewMicrophoneSource(livemode.MicrophoneConfig{Device: request.Input})
	observer := &wailsLiveObserver{app: a, source: source}
	session, err := livemode.NewSession(livemode.SessionConfig{
		Controller: controller, Devices: selected, Source: source, Analyzer: analyzer,
		Tracker: livemode.NewStateTracker(trackerConfig), Generator: generator, Observer: observer,
		MasterBrightness: a.masterBrightnessScale(),
		OnDispatchError: func(event timeline.Event, err error) {
			observer.ReportError(fmt.Sprintf("%s: %v", event.Target, err))
		},
	})
	if err != nil {
		cancel()
		_ = analyzer.Close()
		return err
	}
	done := make(chan struct{})
	a.liveMu.Lock()
	a.liveStop = cancel
	a.liveDone = done
	a.liveSession = session
	a.liveMu.Unlock()

	go func() {
		runErr := session.Run(ctx)
		stopped := LiveStopped{}
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			stopped.Error = runErr.Error()
		}
		wailsruntime.EventsEmit(a.ctx, "live:stopped", stopped)
		a.liveMu.Lock()
		if a.liveSession == session {
			a.liveStop = nil
			a.liveDone = nil
			a.liveSession = nil
		}
		close(done)
		a.liveMu.Unlock()
	}()
	return nil
}

func (a *App) StopLive() {
	a.liveMu.Lock()
	cancel := a.liveStop
	done := a.liveDone
	a.liveMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (a *App) SetLiveStyle(style string) error {
	a.liveMu.Lock()
	session := a.liveSession
	a.liveMu.Unlock()
	if session == nil {
		return fmt.Errorf("Maestro Live is not running")
	}
	return session.SetStyle(style)
}

type wailsLiveObserver struct {
	app       *App
	source    *livemode.MicrophoneSource
	errorMu   sync.Mutex
	lastError time.Time
	warmOnce  sync.Once
}

func (o *wailsLiveObserver) Observe(state livemode.State) {
	o.warmOnce.Do(func() {
		if analyzerbin.Bundled() {
			_ = analyzerbin.MarkWarm()
		}
	})
	wailsruntime.EventsEmit(o.app.ctx, "live:state", LiveState{
		ElapsedMS: state.At.Milliseconds(), Input: o.source.Name(), InputDB: state.InputDB,
		NoiseFloorDB: state.NoiseFloorDB, MarginDB: state.MarginDB, Energy: state.Energy,
		Presence: state.Presence, Low: state.Low, Mid: state.Mid, High: state.High,
		TempoBPM: state.TempoBPM, TempoConfidence: state.TempoConfidence,
		Activity: state.Activity, Intensity: state.Intensity, Dynamics: string(state.Dynamics),
		Novelty: state.Novelty, Active: state.Active, Sustained: state.Sustained,
		Onset: state.Onset, Beat: state.Beat, SectionChange: state.SectionChange,
	})
}

func (o *wailsLiveObserver) ReportError(message string) {
	o.errorMu.Lock()
	defer o.errorMu.Unlock()
	if time.Since(o.lastError) < time.Second {
		return
	}
	o.lastError = time.Now()
	wailsruntime.EventsEmit(o.app.ctx, "live:error", message)
}

func (a *App) Styles() []string {
	return generation.AvailableStyles()
}

func (a *App) GenerationModes() []string {
	return generation.AvailableModes()
}

func (a *App) DynamicsOptions() []string {
	return generation.AvailableDynamics()
}

func (a *App) DiscoverDevices() ([]EditorDevice, error) {
	controller, err := a.lifxController()
	if err != nil {
		return nil, err
	}

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

func (a *App) Generate(audioPath string, style string, target string, generationMode string, dynamics string, assignments []generation.StreamAssignment, editorDevices []EditorDevice) (*EditorSession, error) {
	result, err := a.Analyze(audioPath)
	if err != nil {
		return nil, err
	}
	return a.GenerateFromAnalysis(audioPath, result, style, target, generationMode, dynamics, assignments, editorDevices)
}

// forceTourEnv shows the walkthrough on demand. Development builds have no
// analyzer to prepare, so without this the tour would never appear while it is
// being worked on.
const forceTourEnv = "LIFX_MAESTRO_FORCE_TOUR"

// AnalyzerPreparing reports whether the bundled analyzer still owes its first
// run. The UI shows its intro walkthrough while that happens, so the guide and
// the wait it covers stay coupled: no wait, no interruption.
func (a *App) AnalyzerPreparing() bool {
	if os.Getenv(forceTourEnv) != "" {
		return true
	}
	return analyzerbin.NeedsWarmup()
}

func (a *App) Analyze(audioPath string) (analysis.SongAnalysis, error) {
	if strings.TrimSpace(audioPath) == "" {
		return analysis.SongAnalysis{}, fmt.Errorf("audio path is required")
	}
	if err := audio.ValidateInput(audioPath); err != nil {
		return analysis.SongAnalysis{}, err
	}

	a.cancelAnalyzerWarmup()
	if _, err := a.ensureAnalyzerInstalled(); err != nil {
		return analysis.SongAnalysis{}, fmt.Errorf("prepare analyzer: %w", err)
	}

	analyzer, err := analyzerbin.NewAnalyzer()
	if err != nil {
		return analysis.SongAnalysis{}, fmt.Errorf("prepare analyzer: %w", err)
	}
	result, err := analyzer.Analyze(a.ctx, audioPath)
	if err != nil {
		return analysis.SongAnalysis{}, err
	}
	if analyzerbin.Bundled() {
		_ = analyzerbin.MarkWarm()
	}
	return *result, nil
}

func (a *App) startAnalyzerWarmup() {
	if !analyzerbin.Bundled() {
		return
	}

	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.analyzerWarmupMu.Lock()
	if a.analyzerWarmupDone != nil {
		a.analyzerWarmupMu.Unlock()
		cancel()
		return
	}
	a.analyzerWarmupStop = cancel
	a.analyzerWarmupDone = done
	a.analyzerWarmupMu.Unlock()

	go func() {
		defer func() {
			cancel()
			a.analyzerWarmupMu.Lock()
			if a.analyzerWarmupDone == done {
				a.analyzerWarmupStop = nil
				a.analyzerWarmupDone = nil
			}
			a.analyzerWarmupMu.Unlock()
			close(done)
		}()
		_ = a.warmAnalyzer(ctx)
	}()
}

func (a *App) warmAnalyzer(ctx context.Context) error {
	exePath, err := a.ensureAnalyzerInstalled()
	if err != nil {
		return err
	}
	if !analyzerbin.NeedsWarmup() {
		return nil
	}
	return analyzerbin.Warm(ctx, exePath)
}

func (a *App) waitForAnalyzerWarmup() {
	a.analyzerWarmupMu.Lock()
	done := a.analyzerWarmupDone
	a.analyzerWarmupMu.Unlock()
	if done != nil {
		<-done
	}
}

func (a *App) cancelAnalyzerWarmup() {
	a.analyzerWarmupMu.Lock()
	cancel := a.analyzerWarmupStop
	done := a.analyzerWarmupDone
	a.analyzerWarmupStop = nil
	a.analyzerWarmupDone = nil
	a.analyzerWarmupMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (a *App) ensureAnalyzerInstalled() (string, error) {
	if !analyzerbin.Bundled() {
		return "", nil
	}

	a.analyzerMu.Lock()
	defer a.analyzerMu.Unlock()

	return analyzerbin.EnsureInstalled()
}

func (a *App) GenerateFromAnalysis(audioPath string, song analysis.SongAnalysis, style string, target string, generationMode string, dynamics string, assignments []generation.StreamAssignment, editorDevices []EditorDevice) (*EditorSession, error) {
	if strings.TrimSpace(audioPath) == "" {
		return nil, fmt.Errorf("audio path is required")
	}
	if err := audio.ValidateInput(audioPath); err != nil {
		return nil, err
	}
	if err := song.Validate(); err != nil {
		return nil, err
	}
	if style == "" {
		style = "synthwave"
	}
	if target == "" {
		target = "all"
	}
	if generationMode == "" {
		generationMode = string(generation.GenerationModeSongWide)
	}
	if err := generation.ValidateMode(generation.GenerationMode(generationMode)); err != nil {
		return nil, err
	}
	if dynamics == "" {
		dynamics = string(generation.DynamicsAuto)
	}
	if err := generation.ValidateDynamics(generation.DynamicsOverride(dynamics)); err != nil {
		return nil, err
	}

	return buildEditorSessionWithDevices(audioPath, filepath.Base(audioPath), style, target, generationMode, dynamics, assignments, "generated", song, editorDevices)
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

	a.StopLive()
	a.StopPreview()

	controller, err := a.lifxController()
	if err != nil {
		return err
	}

	restore := setupWailsStateRestore(controller, request.Target)
	audioPlayer, err := audio.NewBeepPlayer(request.AudioPath)
	if err != nil {
		restore()
		return err
	}

	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	lightingPlayer := playback.NewPlayer(controller, playback.Options{
		ClockLabel:       "audio",
		MasterBrightness: a.masterBrightnessScale(),
	})

	a.previewMu.Lock()
	a.previewStop = cancel
	a.previewDone = done
	a.previewAudio = audioPlayer
	a.previewLifx = controller
	a.previewLight = lightingPlayer
	a.previewMu.Unlock()

	go func() {
		defer close(done)
		defer restore()
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

	a.StopLive()
	a.StopPreview()

	audioPlayer, err := audio.NewBeepPlayer(audioPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})

	a.previewMu.Lock()
	a.previewStop = cancel
	a.previewDone = done
	a.previewAudio = audioPlayer
	a.previewLifx = nil
	a.previewLight = nil
	a.previewMu.Unlock()

	go func() {
		defer close(done)
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
	done := a.previewDone
	audioPlayer := a.previewAudio
	a.previewStop = nil
	a.previewDone = nil
	a.previewAudio = nil
	a.previewLifx = nil
	a.previewLight = nil
	a.previewMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if audioPlayer != nil {
		_ = audioPlayer.Stop()
	}
	if done != nil {
		<-done
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
		a.previewDone = nil
		a.previewAudio = nil
		a.previewLifx = nil
		a.previewLight = nil
	}
}

func (a *App) lifxController() (*devices.LifxDeviceController, error) {
	a.lifxMu.Lock()
	defer a.lifxMu.Unlock()

	if a.lifx != nil {
		return a.lifx, nil
	}

	controller, err := devices.NewLifxDeviceController()
	if err != nil {
		return nil, err
	}
	a.lifx = controller
	return controller, nil
}

func buildEditorSession(songPath string, songName string, style string, target string, source string, song analysis.SongAnalysis) (*EditorSession, error) {
	return buildEditorSessionWithDevices(songPath, songName, style, target, string(generation.GenerationModeSongWide), string(generation.DynamicsAuto), nil, source, song, nil)
}

func buildEditorSessionWithDevices(songPath string, songName string, style string, target string, generationMode string, dynamics string, assignments []generation.StreamAssignment, source string, song analysis.SongAnalysis, editorDevices []EditorDevice) (*EditorSession, error) {
	infos := editorDeviceInfosFromEditor(editorDevices)
	if len(infos) == 0 {
		infos = editorDeviceInfos()
	}
	tl, err := generation.Generate(song, generation.Options{
		Name:        songName,
		Target:      target,
		Style:       style,
		Mode:        generation.GenerationMode(generationMode),
		Dynamics:    generation.DynamicsOverride(dynamics),
		Assignments: assignments,
		Devices:     infos,
	})
	if err != nil {
		return nil, err
	}

	return &EditorSession{
		SongPath:   songPath,
		SongName:   songName,
		Style:      style,
		Generation: generationMode,
		Dynamics:   dynamics,
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
		if info.Capabilities.Kind == devices.DeviceKindSwitch || !info.Capabilities.HasColor {
			continue
		}
		result = append(result, EditorDevice{
			ID:       info.ID,
			Label:    info.Label,
			Group:    info.Group,
			Location: info.Location,
			Capabilities: EditorDeviceCapabilities{
				Kind:         info.Capabilities.Kind,
				HasColor:     info.Capabilities.HasColor,
				HasKelvin:    info.Capabilities.HasKelvin,
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
				HasColor:     device.Capabilities.HasColor,
				HasKelvin:    device.Capabilities.HasKelvin,
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
