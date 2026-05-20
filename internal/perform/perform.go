package perform

import (
	"context"
	"fmt"
	"io"

	"lifx-maestro/internal/analysis"
	"lifx-maestro/internal/audio"
	"lifx-maestro/internal/devices"
	"lifx-maestro/internal/generation"
	"lifx-maestro/internal/playback"
)

type Options struct {
	Mode       generation.Mode
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
	if options.Mode == "" {
		options.Mode = generation.ModeDefault
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
		Name:   audio.TimelineName(audioPath),
		Target: options.Target,
		Mode:   options.Mode,
	})
	if err != nil {
		return nil, err
	}

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
		return nil, ctx.Err()
	}

	select {
	case <-audioPlayer.Done():
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return &Result{Analysis: song, Events: len(tl.Events)}, nil
}
