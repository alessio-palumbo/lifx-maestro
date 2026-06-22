package perform

import (
	"context"
	"fmt"
	"io"
	"os"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	"lifx-maestro/internal/playback"
)

type Options struct {
	Style      string
	Target     string
	PythonPath string
	Verbose    bool
	Out        io.Writer
}

type Result struct {
	Analysis *analysis.SongAnalysis
	Events   int
}

func Run(ctx context.Context, audioPath string, controller devices.DeviceController, options Options) (*Result, error) {
	if controller == nil {
		return nil, fmt.Errorf("device controller is required")
	}
	if err := audio.ValidateInput(audioPath); err != nil {
		return nil, err
	}
	if options.Target == "" {
		options.Target = "all"
	}

	analyzer := analysis.NewAnalyzer()
	if options.PythonPath != "" {
		analyzer.PythonPath = options.PythonPath
	}

	if options.Verbose && options.Out != nil {
		fmt.Fprintln(options.Out, "[perform] analyzing audio")
	}
	song, err := analyzer.Analyze(ctx, audioPath)
	if err != nil {
		return nil, err
	}

	if options.Verbose && options.Out != nil {
		fmt.Fprintf(options.Out, "[perform] generating timeline bpm=%.3f beats=%d\n", song.BPM, len(song.Beats))
	}
	tl, err := generation.Generate(*song, generation.Options{
		Name:    audio.TimelineName(audioPath),
		Target:  options.Target,
		Style:   options.Style,
		Devices: deviceInfos(controller),
	})
	if err != nil {
		return nil, err
	}

	restore := setupStateRestore(controller, options.Target)
	defer restore()

	audioPlayer, err := audio.NewBeepPlayer(audioPath)
	if err != nil {
		return nil, err
	}
	defer audioPlayer.Stop()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lightingDone := make(chan error, 1)
	lightingPlayer := playback.NewPlayer(controller, playback.Options{
		Verbose:    options.Verbose,
		Out:        options.Out,
		ClockLabel: "audio",
	})
	go func() {
		lightingDone <- lightingPlayer.PlayWithClock(ctx, tl, audioPlayer)
	}()

	if options.Verbose && options.Out != nil {
		fmt.Fprintf(options.Out, "[perform] starting audio duration=%s events=%d\n", playback.FormatOffset(audioPlayer.Duration()), len(tl.Events))
	}
	if err := audioPlayer.Play(); err != nil {
		cancel()
		<-lightingDone
		return nil, err
	}

	select {
	case err := <-lightingDone:
		if err != nil {
			cancel()
			return nil, err
		}
	case <-ctx.Done():
		cancel()
		<-lightingDone
		return nil, ctx.Err()
	}

	select {
	case <-audioPlayer.Done():
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}

	return &Result{Analysis: song, Events: len(tl.Events)}, nil
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

func deviceInfos(controller devices.DeviceController) []devices.DeviceInfo {
	provider, ok := controller.(devices.CapabilityProvider)
	if !ok {
		return nil
	}
	infos, err := provider.Devices()
	if err != nil {
		return nil
	}
	return infos
}
