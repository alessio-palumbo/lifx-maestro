package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	"lifx-maestro/internal/perform"
	"lifx-maestro/internal/playback"
	"lifx-maestro/internal/timeline"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "maestro:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "analyze":
		return analyze(args[1:])
	case "generate":
		return generate(args[1:])
	case "perform":
		return performCommand(args[1:])
	case "play":
		return play(args[1:])
	default:
		return usage()
	}
}

func analyze(args []string) error {
	flags := flag.NewFlagSet("analyze", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	pythonPath := flags.String("python", "python3", "python executable")

	if err := flags.Parse(interspersedFlags(args, map[string]bool{"python": true})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: maestro analyze [--python python3] <song.mp3|song.wav>")
	}

	audioPath := flags.Arg(0)
	if err := audio.ValidateInput(audioPath); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	analyzer := analysis.NewAnalyzer()
	analyzer.PythonPath = *pythonPath

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
}

func generate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	outputPath := flags.String("output", "", "timeline JSON output path")
	mode := flags.String("mode", string(generation.ModeDefault), "generation mode: default, ambient, energetic")
	target := flags.String("target", "all", "timeline target selector")
	pythonPath := flags.String("python", "python3", "python executable")

	if err := flags.Parse(interspersedFlags(args, map[string]bool{
		"output": true,
		"mode":   true,
		"target": true,
		"python": true,
	})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: maestro generate [--output projects/song.json] [--mode default|ambient|energetic] [--target all] [--python python3] <song.mp3|song.wav>")
	}

	audioPath := flags.Arg(0)
	if err := audio.ValidateInput(audioPath); err != nil {
		return err
	}
	if *outputPath == "" {
		*outputPath = audio.DefaultTimelinePath(audioPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	analyzer := analysis.NewAnalyzer()
	analyzer.PythonPath = *pythonPath

	result, err := analyzer.Analyze(ctx, audioPath)
	if err != nil {
		return err
	}

	tl, err := generation.Generate(*result, generation.Options{
		Name:   audio.TimelineName(audioPath),
		Target: *target,
		Mode:   generation.Mode(*mode),
	})
	if err != nil {
		return err
	}

	if err := timeline.Save(*outputPath, tl); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "generated %s (%d events, bpm %.3f)\n", *outputPath, len(tl.Events), result.BPM)
	return nil
}

func performCommand(args []string) error {
	flags := flag.NewFlagSet("perform", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dryRun := flags.Bool("dry-run", false, "use mock device controller")
	verbose := flags.Bool("verbose", false, "print synchronization details")
	mode := flags.String("mode", string(generation.ModeDefault), "generation mode: default, ambient, energetic")
	devicesTarget := flags.String("devices", "all", "device selector")
	pythonPath := flags.String("python", "python3", "python executable")

	if err := flags.Parse(interspersedFlags(args, map[string]bool{
		"mode":    true,
		"devices": true,
		"python":  true,
	})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: maestro perform [--dry-run] [--verbose] [--mode default|ambient|energetic] [--devices all] [--python python3] <song.mp3>")
	}

	var controller devices.DeviceController
	if *dryRun {
		controller = devices.NewMockDeviceController(os.Stdout)
	} else {
		lifxController, err := devices.NewLifxDeviceController()
		if err != nil {
			return err
		}
		defer lifxController.Close()
		controller = lifxController
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	result, err := perform.Run(ctx, flags.Arg(0), controller, perform.Options{
		Mode:       generation.Mode(*mode),
		Target:     *devicesTarget,
		PythonPath: *pythonPath,
		Verbose:    *verbose,
		Out:        os.Stdout,
	})
	if err != nil {
		return err
	}

	if *verbose {
		fmt.Fprintf(os.Stdout, "[perform] complete bpm=%.3f events=%d\n", result.Analysis.BPM, result.Events)
	}
	return nil
}

func play(args []string) error {
	flags := flag.NewFlagSet("play", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dryRun := flags.Bool("dry-run", false, "use mock device controller")
	verbose := flags.Bool("verbose", false, "print timeline details before playback")

	if err := flags.Parse(interspersedFlags(args, map[string]bool{})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: maestro play [--dry-run] [--verbose] <timeline.json>")
	}

	tl, err := timeline.Load(flags.Arg(0))
	if err != nil {
		return err
	}

	if *verbose {
		fmt.Fprintf(os.Stdout, "timeline=%q duration_ms=%d events=%d dry_run=%t\n", tl.Name, tl.DurationMS, len(tl.Events), *dryRun)
	}

	var controller devices.DeviceController
	if *dryRun {
		controller = devices.NewMockDeviceController(os.Stdout)
	} else {
		lifxController, err := devices.NewLifxDeviceController()
		if err != nil {
			return err
		}
		defer lifxController.Close()
		controller = lifxController
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	player := playback.NewPlayer(controller, playback.Options{
		DryRun:  *dryRun,
		Verbose: *verbose,
		Out:     os.Stdout,
	})

	return player.Play(ctx, tl)
}

func usage() error {
	return fmt.Errorf("usage: maestro <analyze|generate|play> [options]")
}

func interspersedFlags(args []string, valueFlags map[string]bool) []string {
	var flags []string
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)
		name := flagName(arg)
		if valueFlags[name] && !hasInlineFlagValue(arg) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	return append(flags, positional...)
}

func flagName(arg string) string {
	for len(arg) > 0 && arg[0] == '-' {
		arg = arg[1:]
	}
	for i, r := range arg {
		if r == '=' {
			return arg[:i]
		}
	}
	return arg
}

func hasInlineFlagValue(arg string) bool {
	for _, r := range arg {
		if r == '=' {
			return true
		}
	}
	return false
}
