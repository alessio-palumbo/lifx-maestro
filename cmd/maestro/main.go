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
	"sync"
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
			generateCommand(),
			liveCommand(),
			performCommand(),
			playCommand(),
			stylesCommand(),
		},
	}
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
			&cli.BoolFlag{Name: "dry-run", Usage: "use mock device controller"},
			&cli.BoolFlag{Name: "verbose", Usage: "print periodically refreshed analysis diagnostics"},
			&cli.StringFlag{Name: "python", Usage: "python executable (overrides the bundled analyzer)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("usage: maestro live [--input name] [--target all] [--style synthwave] [--verbose]")
			}
			if cmd.Bool("list-inputs") {
				return printInputDevices()
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
			resolvedTarget := deviceIDs(selected)
			restore := setupStateRestore(controller, resolvedTarget)
			defer restore()
			if err := powerOnDevices(controller, selected); err != nil {
				return err
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
			player := playback.NewPlayer(controller, playback.Options{Verbose: false, Out: os.Stdout})
			dispatcher := livemode.NewDispatcher(player, func(event timeline.Event, err error) {
				fmt.Fprintf(os.Stderr, "maestro: live event target=%s: %v\n", event.Target, err)
			})
			observer := newLiveDiagnostics(cmd.Bool("verbose"), source, cmd.String("target"), os.Stdout)
			engine, err := livemode.NewEngine(livemode.EngineConfig{
				Source:    source,
				Analyzer:  analyzer,
				Generator: generator,
				Sink:      dispatcher,
				Observer:  observer,
			})
			if err != nil {
				_ = analyzer.Close()
				return err
			}
			fmt.Fprintf(os.Stdout, "listening on %s for target %s; press Ctrl-C to stop\n", source.Name(), cmd.String("target"))
			return engine.Run(ctx)
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

func deviceIDs(infos []devices.DeviceInfo) string {
	ids := make([]string, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ID)
	}
	return strings.Join(ids, ",")
}

func powerOnDevices(controller devices.DeviceController, infos []devices.DeviceInfo) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(infos))
	for _, info := range infos {
		info := info
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := controller.PowerOn(info.ID); err != nil {
				errs <- fmt.Errorf("power on %s: %w", displayName(info), err)
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

type liveDiagnostics struct {
	enabled bool
	source  livemode.AudioSource
	target  string
	out     io.Writer
	last    time.Duration
	printed bool
}

func newLiveDiagnostics(enabled bool, source livemode.AudioSource, target string, out io.Writer) livemode.Observer {
	if !enabled {
		return nil
	}
	return &liveDiagnostics{enabled: true, source: source, target: target, out: out}
}

func (d *liveDiagnostics) Observe(state livemode.State) {
	if !d.printed {
		fmt.Fprintf(d.out, "input=%s target=%s\n", d.source.Name(), d.target)
		d.printed = true
	}
	if state.At-d.last < 500*time.Millisecond {
		return
	}
	d.last = state.At
	beat := ""
	if state.Beat || state.Onset {
		beat = " *"
	}
	fmt.Fprintf(d.out, "[live %s] noise=%5.1fdB energy=%.2f low=%.2f mid=%.2f high=%.2f tempo=%5.1f%s\n",
		playback.FormatOffset(state.At), state.NoiseFloorDB, state.Energy, state.Low, state.Mid, state.High, state.TempoBPM, beat)
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
