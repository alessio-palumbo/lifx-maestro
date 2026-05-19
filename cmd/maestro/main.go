package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"lifx-maestro/internal/devices"
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
	case "play":
		return play(args[1:])
	default:
		return usage()
	}
}

func play(args []string) error {
	flags := flag.NewFlagSet("play", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dryRun := flags.Bool("dry-run", false, "use mock device controller")
	verbose := flags.Bool("verbose", false, "print timeline details before playback")

	if err := flags.Parse(args); err != nil {
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
	return fmt.Errorf("usage: maestro play [--dry-run] [--verbose] <timeline.json>")
}
