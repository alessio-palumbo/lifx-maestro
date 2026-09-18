package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/analyzerbin"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	livemode "lifx-maestro/internal/live"
	"lifx-maestro/internal/perform"
	"lifx-maestro/internal/playback"
	"lifx-maestro/internal/timeline"
)

func main() {
	if err := newCommand().Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "maestro:", err)
		os.Exit(1)
	}
}

func newCommand() *cli.Command {
	return &cli.Command{
		Name:  "maestro",
		Usage: "generate and play synchronized smart-light choreographies",
		Commands: []*cli.Command{
			analyzeCommand(),
			devicesCommand(),
			evaluateLiveCommand(),
			generateCommand(),
			liveCommand(),
			performCommand(),
			playCommand(),
			stylesCommand(),
		},
	}
}

func evaluateLiveCommand() *cli.Command {
	return &cli.Command{
		Name:      "evaluate-live",
		Usage:     "evaluate the Live pipeline reproducibly from an audio file",
		ArgsUsage: "<song.mp3|song.wav>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "style", Value: "synthwave", Usage: "generation style"},
			&cli.StringFlag{Name: "intensity", Value: string(generation.DynamicsAuto), Usage: "show intensity (auto, calm, balanced, or energetic)"},
			&cli.StringFlag{Name: "sensitivity", Value: string(livemode.SensitivityLow), Usage: "input sensitivity (low, normal, or high)"},
			&cli.StringFlag{Name: "device-kind", Value: "all", Usage: "synthetic target kind (single_zone, multizone, matrix, or all)"},
			&cli.Float64Flag{Name: "input-level", Value: livemode.DefaultEvaluationInputDB, Usage: "simulated microphone RMS level in dB"},
			&cli.StringFlag{Name: "output", Usage: "write the complete state and event trace as JSON"},
			&cli.BoolFlag{Name: "skip-offline", Usage: "skip comparison with full-track analysis"},
			&cli.StringFlag{Name: "python", Usage: "python executable (overrides the bundled analyzer)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			audioPath, err := singleArg(cmd, "maestro evaluate-live [options] <song.mp3|song.wav>")
			if err != nil {
				return err
			}
			if err := audio.ValidateInput(audioPath); err != nil {
				return err
			}
			trackerConfig, err := livemode.TrackerConfigForSensitivity(cmd.String("sensitivity"))
			if err != nil {
				return err
			}
			selected, err := evaluationDevices(cmd.String("device-kind"))
			if err != nil {
				return err
			}
			analyzerConfig, err := newAnalyzer(cmd)
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr, "decoding audio...")
			pcm, err := audio.DecodePCM(audioPath)
			if err != nil {
				return err
			}
			var offline *analysis.SongAnalysis
			if !cmd.Bool("skip-offline") {
				fmt.Fprintln(os.Stderr, "running full-track comparison...")
				offline, err = analyzerConfig.Analyze(ctx, audioPath)
				if err != nil {
					return fmt.Errorf("run offline comparison: %w", err)
				}
			}
			analyzer, err := livemode.NewPythonAnalyzer(ctx, analyzerConfig)
			if err != nil {
				return err
			}
			generator, err := livemode.NewGenerator(livemode.GeneratorConfig{
				Style:     cmd.String("style"),
				Intensity: generation.DynamicsOverride(cmd.String("intensity")),
				Devices:   selected,
			})
			if err != nil {
				_ = analyzer.Close()
				return err
			}
			fmt.Fprintln(os.Stderr, "running rolling Live pipeline...")
			trace, err := livemode.EvaluatePCM(ctx, audioPath, pcm.Samples, pcm.SampleRate, livemode.EvaluationConfig{
				Analyzer: analyzer, Tracker: livemode.NewStateTracker(trackerConfig), Generator: generator,
				InputLevelDB: cmd.Float64("input-level"),
			})
			if err != nil {
				return err
			}
			livemode.AddOfflineComparison(trace, offline, livemode.DefaultEvaluationBucket)
			livemode.WriteEvaluationReport(os.Stdout, trace)
			if output := cmd.String("output"); output != "" {
				if err := livemode.SaveEvaluationTrace(output, trace); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "trace                %s\n", output)
			}
			return nil
		},
	}
}

func evaluationDevices(kind string) ([]devices.DeviceInfo, error) {
	infos, err := devices.NewMockDeviceController(io.Discard).Devices()
	if err != nil {
		return nil, err
	}
	if kind == "all" {
		return devices.ControllableLights(infos), nil
	}
	var wanted devices.DeviceKind
	switch kind {
	case "single_zone":
		wanted = devices.DeviceKindSingleZone
	case "multizone", "multi_zone":
		wanted = devices.DeviceKindMultiZone
	case "matrix":
		wanted = devices.DeviceKindMatrix
	default:
		return nil, fmt.Errorf("unsupported evaluation device kind %q (use single_zone, multizone, matrix, or all)", kind)
	}
	selected := make([]devices.DeviceInfo, 0, 1)
	for _, info := range infos {
		if info.Capabilities.Kind == wanted {
			selected = append(selected, info)
		}
	}
	return selected, nil
}

func liveCommand() *cli.Command {
	return &cli.Command{
		Name:      "live",
		Usage:     "drive lights continuously from microphone audio",
		ArgsUsage: "",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "input", Usage: "microphone name or ID (default system input when omitted)"},
			&cli.BoolFlag{Name: "list-inputs", Usage: "list available microphone inputs and exit"},
			&cli.StringFlag{Name: "target", Value: "all", Usage: "target selector"},
			&cli.StringFlag{Name: "style", Value: "synthwave", Usage: "generation style"},
			&cli.StringFlag{Name: "intensity", Value: string(generation.DynamicsAuto), Usage: "show intensity (auto, calm, balanced, or energetic)"},
			&cli.StringFlag{Name: "sensitivity", Value: string(livemode.SensitivityLow), Usage: "microphone sensitivity (low, normal, or high)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "use mock device controller"},
			&cli.BoolFlag{Name: "verbose", Usage: "print periodically refreshed analysis diagnostics"},
			&cli.StringFlag{Name: "python", Usage: "python executable (overrides the bundled analyzer)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("usage: maestro live [--input name] [--target all] [--style synthwave] [--intensity auto] [--sensitivity low] [--verbose]")
			}
			if cmd.Bool("list-inputs") {
				return printInputDevices()
			}
			trackerConfig, err := livemode.TrackerConfigForSensitivity(cmd.String("sensitivity"))
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			controller, provider, closeDeviceController, err := liveDeviceController(cmd.Bool("dry-run"))
			if err != nil {
				return err
			}
			defer closeDeviceController()

			infos, err := provider.Devices()
			if err != nil {
				return err
			}
			selected, err := devices.SelectDeviceInfos(infos, cmd.String("target"))
			if err != nil {
				return err
			}
			selected = devices.ControllableLights(selected)
			if len(selected) == 0 {
				return fmt.Errorf("target %q contains no controllable lights", cmd.String("target"))
			}
			analyzerConfig, err := newAnalyzer(cmd)
			if err != nil {
				return err
			}
			analyzer, err := livemode.NewPythonAnalyzer(ctx, analyzerConfig)
			if err != nil {
				return err
			}
			generator, err := livemode.NewGenerator(livemode.GeneratorConfig{
				Style:     cmd.String("style"),
				Intensity: generation.DynamicsOverride(cmd.String("intensity")),
				Devices:   selected,
			})
			if err != nil {
				_ = analyzer.Close()
				return err
			}

			source := livemode.NewMicrophoneSource(livemode.MicrophoneConfig{Device: cmd.String("input")})
			session, err := livemode.NewSession(livemode.SessionConfig{
				Controller: controller, Devices: selected, Source: source, Analyzer: analyzer,
				Tracker: livemode.NewStateTracker(trackerConfig), Generator: generator,
				OnDispatchError: func(event timeline.Event, err error) {
					fmt.Fprintf(os.Stderr, "maestro: live event target=%s: %v\n", event.Target, err)
				},
			})
			if err != nil {
				_ = analyzer.Close()
				return err
			}
			session.SetObserver(newLiveDiagnostics(cmd.Bool("verbose"), source, cmd.String("target"), session.Dispatcher(), os.Stdout))
			fmt.Fprintf(os.Stdout, "listening on %s for target %s; press Ctrl-C to stop\n", source.Name(), cmd.String("target"))
			return session.Run(ctx)
		},
	}
}

func printInputDevices() error {
	inputs, err := livemode.ListInputDevices()
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no microphone inputs found")
	}
	for _, input := range inputs {
		marker := ""
		if input.Default {
			marker = " (default)"
		}
		fmt.Fprintf(os.Stdout, "%s%s\n  %s\n", input.Name, marker, input.ID)
	}
	return nil
}

func liveDeviceController(dryRun bool) (devices.DeviceController, devices.CapabilityProvider, func(), error) {
	if dryRun {
		controller := devices.NewMockDeviceController(os.Stdout)
		return controller, controller, func() {}, nil
	}
	controller, err := devices.NewLifxDeviceController()
	if err != nil {
		return nil, nil, nil, err
	}
	return controller, controller, func() { closeController(controller) }, nil
}

type liveDiagnostics struct {
	source        livemode.AudioSource
	target        string
	dispatcher    *livemode.Dispatcher
	out           io.Writer
	last          time.Duration
	lastDispatch  livemode.DispatcherStats
	printed       bool
	accents       int
	onsets        int
	beats         int
	sections      int
	generated     int
	droppedEvents int
}

func newLiveDiagnostics(enabled bool, source livemode.AudioSource, target string, dispatcher *livemode.Dispatcher, out io.Writer) livemode.Observer {
	if !enabled {
		return nil
	}
	return &liveDiagnostics{source: source, target: target, dispatcher: dispatcher, out: out}
}

func (d *liveDiagnostics) ObserveOutput(activity livemode.OutputActivity) {
	d.generated += activity.GeneratedEvents
	d.droppedEvents += activity.DroppedEvents
}

func (d *liveDiagnostics) Observe(state livemode.State) {
	if !d.printed {
		fmt.Fprintf(d.out, "input=%s target=%s\n", d.source.Name(), d.target)
		d.printed = true
	}
	if state.Active && (state.Beat || state.Onset) {
		d.accents++
		if state.Onset {
			d.onsets++
		}
		if state.Beat {
			d.beats++
		}
	}
	if state.SectionChange {
		d.sections++
	}
	if state.At-d.last < 500*time.Millisecond {
		return
	}
	d.last = state.At
	gate := "closed"
	if state.Active {
		gate = "open"
	}
	motion := "accent-only"
	if state.Sustained {
		motion = "ambient"
	}
	dispatch := livemode.DispatcherStats{}
	if d.dispatcher != nil {
		dispatch = d.dispatcher.Stats()
	}
	sent := dispatch.Sent - d.lastDispatch.Sent
	replaced := dispatch.Replaced - d.lastDispatch.Replaced + uint64(d.droppedEvents)
	errors := dispatch.Errors - d.lastDispatch.Errors
	fmt.Fprintf(d.out, "[live %s] level=%5.1fdB floor=%5.1fdB margin=%4.1fdB energy=%.2f presence=%.2f low=%.2f mid=%.2f high=%.2f tempo=%5.1f confidence=%.2f activity=%.2f intensity=%.2f dynamics=%s novelty=%.2f sections=%d gate=%s motion=%s accents=%d(onset=%d beat=%d) generated=%d sent=%d replaced=%d errors=%d\n",
		playback.FormatOffset(state.At), state.InputDB, state.NoiseFloorDB, state.MarginDB, state.Energy, state.Presence, state.Low, state.Mid, state.High, state.TempoBPM, state.TempoConfidence, state.Activity, state.Intensity, state.Dynamics, state.Novelty, d.sections, gate, motion, d.accents, d.onsets, d.beats, d.generated, sent, replaced, errors)
	d.lastDispatch = dispatch
	d.accents = 0
	d.onsets = 0
	d.beats = 0
	d.sections = 0
	d.generated = 0
	d.droppedEvents = 0
}

func devicesCommand() *cli.Command {
	return &cli.Command{
		Name:  "devices",
		Usage: "discover LIFX devices and print capabilities",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "print mock device capabilities"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var provider devices.CapabilityProvider
			if cmd.Bool("dry-run") {
				provider = devices.NewMockDeviceController(os.Stdout)
			} else {
				lifxController, err := devices.NewLifxDeviceController()
				if err != nil {
					return err
				}
				defer closeController(lifxController)
				provider = lifxController
			}

			infos, err := provider.Devices()
			if err != nil {
				return err
			}
			for _, info := range infos {
				fmt.Fprintf(os.Stdout, "%-18s %-11s %s\n", displayName(info), info.Capabilities.Kind, capabilitySummary(info.Capabilities))
			}
			return nil
		},
	}
}

func analyzeCommand() *cli.Command {
	return &cli.Command{
		Name:      "analyze",
		Usage:     "analyze an audio file and print analysis JSON",
		ArgsUsage: "<song.mp3|song.wav>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "python", Usage: "python executable (overrides the bundled analyzer)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			audioPath, err := singleArg(cmd, "maestro analyze [--python analyzer/.venv/bin/python] <song.mp3|song.wav>")
			if err != nil {
				return err
			}
			if err := audio.ValidateInput(audioPath); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			analyzer, err := newAnalyzer(cmd)
			if err != nil {
				return err
			}

			result, err := analyzer.Analyze(ctx, audioPath)
			if err != nil {
				return err
			}

			encoded, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("encode analysis JSON: %w", err)
			}
			fmt.Fprintln(os.Stdout, string(encoded))
			return nil
		},
	}
}

func generateCommand() *cli.Command {
	return &cli.Command{
		Name:      "generate",
		Usage:     "analyze audio and write a generated timeline JSON file",
		ArgsUsage: "<song.mp3|song.wav>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output", Usage: "timeline JSON output path"},
			&cli.StringFlag{Name: "style", Usage: "generation style"},
			&cli.StringFlag{Name: "generation", Value: string(generation.GenerationModeSongWide), Usage: "generation mode (song_wide or musical_layers)"},
			&cli.StringFlag{Name: "intensity", Value: string(generation.DynamicsAuto), Usage: "show intensity (auto, calm, balanced, or energetic)"},
			&cli.StringFlag{Name: "target", Value: "all", Usage: "timeline target selector"},
			&cli.StringFlag{Name: "python", Usage: "python executable (overrides the bundled analyzer)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			audioPath, err := singleArg(cmd, "maestro generate [--output projects/song.json] [--style synthwave] [--generation song_wide] [--intensity auto] [--target all] [--python analyzer/.venv/bin/python] <song.mp3|song.wav>")
			if err != nil {
				return err
			}
			if err := audio.ValidateInput(audioPath); err != nil {
				return err
			}

			outputPath := cmd.String("output")
			if outputPath == "" {
				outputPath = audio.DefaultTimelinePath(audioPath)
			}

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			analyzer, err := newAnalyzer(cmd)
			if err != nil {
				return err
			}

			result, err := analyzer.Analyze(ctx, audioPath)
			if err != nil {
				return err
			}

			tl, err := generation.Generate(*result, generation.Options{
				Name:     audio.TimelineName(audioPath),
				Target:   cmd.String("target"),
				Style:    cmd.String("style"),
				Mode:     generation.GenerationMode(cmd.String("generation")),
				Dynamics: generation.DynamicsOverride(cmd.String("intensity")),
			})
			if err != nil {
				return err
			}

			if err := timeline.Save(outputPath, tl); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "generated %s (%d events, bpm %.3f)\n", outputPath, len(tl.Events), result.BPM)
			return nil
		},
	}
}

func performCommand() *cli.Command {
	return &cli.Command{
		Name:      "perform",
		Usage:     "analyze, generate, play audio, and perform synchronized lighting",
		ArgsUsage: "<song.mp3>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "use mock device controller"},
			&cli.BoolFlag{Name: "verbose", Usage: "print synchronization details"},
			&cli.StringFlag{Name: "style", Usage: "generation style"},
			&cli.StringFlag{Name: "generation", Value: string(generation.GenerationModeSongWide), Usage: "generation mode (song_wide or musical_layers)"},
			&cli.StringFlag{Name: "intensity", Value: string(generation.DynamicsAuto), Usage: "show intensity (auto, calm, balanced, or energetic)"},
			&cli.StringFlag{Name: "target", Value: "all", Usage: "target selector"},
			&cli.StringFlag{Name: "python", Usage: "python executable (overrides the bundled analyzer)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			audioPath, err := singleArg(cmd, "maestro perform [--dry-run] [--verbose] [--style synthwave] [--generation song_wide] [--intensity auto] [--target all] [--python analyzer/.venv/bin/python] <song.mp3>")
			if err != nil {
				return err
			}
			target := cmd.String("target")

			var controller devices.DeviceController
			if cmd.Bool("dry-run") {
				controller = devices.NewMockDeviceController(os.Stdout)
			} else {
				lifxController, err := devices.NewLifxDeviceController()
				if err != nil {
					return err
				}
				defer closeController(lifxController)
				controller = lifxController
			}

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			analyzer, err := newAnalyzer(cmd)
			if err != nil {
				return err
			}

			result, err := perform.Run(ctx, audioPath, controller, perform.Options{
				Style:    cmd.String("style"),
				Target:   target,
				Mode:     generation.GenerationMode(cmd.String("generation")),
				Dynamics: generation.DynamicsOverride(cmd.String("intensity")),
				Analyzer: analyzer,
				Verbose:  cmd.Bool("verbose"),
				Out:      os.Stdout,
			})
			if err != nil {
				return err
			}

			if cmd.Bool("verbose") {
				fmt.Fprintf(os.Stdout, "[perform] complete bpm=%.3f events=%d\n", result.Analysis.BPM, result.Events)
			}
			return nil
		},
	}
}

func stylesCommand() *cli.Command {
	return &cli.Command{
		Name:  "styles",
		Usage: "list available generation styles",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, name := range generation.AvailableStyles() {
				fmt.Fprintln(os.Stdout, name)
			}
			return nil
		},
	}
}

func playCommand() *cli.Command {
	return &cli.Command{
		Name:      "play",
		Usage:     "play a timeline JSON file",
		ArgsUsage: "<timeline.json>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "use mock device controller"},
			&cli.BoolFlag{Name: "verbose", Usage: "print timeline and scheduler details"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			timelinePath, err := singleArg(cmd, "maestro play [--dry-run] [--verbose] <timeline.json>")
			if err != nil {
				return err
			}

			tl, err := timeline.Load(timelinePath)
			if err != nil {
				return err
			}

			if cmd.Bool("verbose") {
				fmt.Fprintf(os.Stdout, "timeline=%q duration_ms=%d events=%d dry_run=%t\n", tl.Name, tl.DurationMS, len(tl.Events), cmd.Bool("dry-run"))
			}

			var controller devices.DeviceController
			if cmd.Bool("dry-run") {
				controller = devices.NewMockDeviceController(os.Stdout)
			} else {
				lifxController, err := devices.NewLifxDeviceController()
				if err != nil {
					return err
				}
				defer closeController(lifxController)
				controller = lifxController
			}
			defer setupStateRestore(controller, "all")()

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			player := playback.NewPlayer(controller, playback.Options{
				DryRun:  cmd.Bool("dry-run"),
				Verbose: cmd.Bool("verbose"),
				Out:     os.Stdout,
			})

			return player.Play(ctx, tl)
		},
	}
}

func singleArg(cmd *cli.Command, usage string) (string, error) {
	if cmd.Args().Len() != 1 {
		return "", fmt.Errorf("usage: %s", usage)
	}
	return cmd.Args().First(), nil
}

func setupStateRestore(controller devices.DeviceController, target string) func() {
	restorer, ok := controller.(devices.StateRestorer)
	if !ok {
		return func() {}
	}

	if err := restorer.CaptureState(target); err != nil {
		fmt.Fprintf(os.Stderr, "maestro: capture state: %v\n", err)
		return func() {}
	}

	return func() {
		if err := restorer.RestoreState(); err != nil {
			fmt.Fprintf(os.Stderr, "maestro: restore state: %v\n", err)
		}
	}
}

func closeController(controller interface{ Close() error }) {
	if err := controller.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "maestro: close controller: %v\n", err)
	}
}

// newAnalyzer resolves the analyzer for this build, honouring --python as an
// explicit override of the bundled analyzer.
func newAnalyzer(cmd *cli.Command) (analysis.Analyzer, error) {
	if override := cmd.String("python"); override != "" {
		if !filepath.IsAbs(override) && strings.ContainsAny(override, `/\\`) {
			absolute, err := filepath.Abs(override)
			if err != nil {
				return analysis.Analyzer{}, fmt.Errorf("resolve Python executable: %w", err)
			}
			override = absolute
		}
		return analysis.Analyzer{
			PythonPath: override,
			ScriptPath: analyzerbin.DevScriptPath(),
		}, nil
	}
	return analyzerbin.NewAnalyzer()
}

func displayName(info devices.DeviceInfo) string {
	if info.Label != "" {
		return info.Label
	}
	if info.ID != "" {
		return info.ID
	}
	return "unknown"
}

func capabilitySummary(capabilities devices.DeviceCapabilities) string {
	switch capabilities.Kind {
	case devices.DeviceKindMultiZone:
		return fmt.Sprintf("%d zones", capabilities.ZoneCount)
	case devices.DeviceKindMatrix:
		if capabilities.MatrixLength > 1 {
			return fmt.Sprintf("%dx%d x%d", capabilities.MatrixWidth, capabilities.MatrixHeight, capabilities.MatrixLength)
		}
		return fmt.Sprintf("%dx%d", capabilities.MatrixWidth, capabilities.MatrixHeight)
	case devices.DeviceKindSwitch:
		return "switch"
	default:
		if capabilities.HasColor {
			return "color"
		}
		return "white"
	}
}
