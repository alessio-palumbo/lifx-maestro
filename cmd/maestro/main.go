package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"github.com/urfave/cli/v3"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
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
			performCommand(),
			playCommand(),
			stylesCommand(),
		},
	}
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
			&cli.StringFlag{Name: "python", Value: defaultPythonPath(), Usage: "python executable"},
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

			analyzer := analysis.NewAnalyzer()
			analyzer.PythonPath = cmd.String("python")

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
			&cli.StringFlag{Name: "target", Value: "all", Usage: "timeline target selector"},
			&cli.StringFlag{Name: "python", Value: defaultPythonPath(), Usage: "python executable"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			audioPath, err := singleArg(cmd, "maestro generate [--output projects/song.json] [--style synthwave] [--target all] [--python analyzer/.venv/bin/python] <song.mp3|song.wav>")
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

			analyzer := analysis.NewAnalyzer()
			analyzer.PythonPath = cmd.String("python")

			result, err := analyzer.Analyze(ctx, audioPath)
			if err != nil {
				return err
			}

			tl, err := generation.Generate(*result, generation.Options{
				Name:   audio.TimelineName(audioPath),
				Target: cmd.String("target"),
				Style:  cmd.String("style"),
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
			&cli.StringFlag{Name: "target", Value: "all", Usage: "target selector"},
			&cli.StringFlag{Name: "python", Value: defaultPythonPath(), Usage: "python executable"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			audioPath, err := singleArg(cmd, "maestro perform [--dry-run] [--verbose] [--style synthwave] [--target all] [--python analyzer/.venv/bin/python] <song.mp3>")
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
			defer setupStateRestore(controller, target)()

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
			defer stop()

			result, err := perform.Run(ctx, audioPath, controller, perform.Options{
				Style:      cmd.String("style"),
				Target:     target,
				PythonPath: cmd.String("python"),
				Verbose:    cmd.Bool("verbose"),
				Out:        os.Stdout,
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

func defaultPythonPath() string {
	candidates := []string{
		"analyzer/.venv/bin/python",
		".venv/bin/python",
	}
	if runtime.GOOS == "windows" {
		candidates = []string{
			"analyzer/.venv/Scripts/python.exe",
			".venv/Scripts/python.exe",
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
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
	default:
		if capabilities.HasColor {
			return "color"
		}
		return "white"
	}
}
