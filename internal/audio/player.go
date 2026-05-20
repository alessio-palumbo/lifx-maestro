package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type AudioPlayer interface {
	Play() error
	Pause() error
	Stop() error
	Position() time.Duration
	Duration() time.Duration
}

type BeepPlayer struct {
	streamer beep.StreamSeekCloser
	format   beep.Format
	ctrl     *beep.Ctrl
	done     chan struct{}
	once     sync.Once
	closed   bool
	mu       sync.Mutex
}

func NewBeepPlayer(path string) (*BeepPlayer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}

	var (
		streamer beep.StreamSeekCloser
		format   beep.Format
	)

	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		streamer, format, err = mp3.Decode(file)
	default:
		file.Close()
		return nil, fmt.Errorf("audio playback currently supports MP3 files only")
	}
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("decode audio file: %w", err)
	}

	if err := initSpeaker(format.SampleRate); err != nil {
		streamer.Close()
		return nil, err
	}

	player := &BeepPlayer{
		streamer: streamer,
		format:   format,
		done:     make(chan struct{}),
	}
	player.ctrl = &beep.Ctrl{
		Streamer: beep.Seq(streamer, beep.Callback(func() {
			player.closeDone()
		})),
		Paused: true,
	}

	speaker.Play(player.ctrl)
	return player, nil
}

func (p *BeepPlayer) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("audio player is stopped")
	}

	speaker.Lock()
	p.ctrl.Paused = false
	speaker.Unlock()
	return nil
}

func (p *BeepPlayer) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}

	speaker.Lock()
	p.ctrl.Paused = true
	speaker.Unlock()
	return nil
}

func (p *BeepPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}

	speaker.Lock()
	p.ctrl.Streamer = nil
	speaker.Unlock()

	p.closed = true
	p.closeDone()
	return p.streamer.Close()
}

func (p *BeepPlayer) Position() time.Duration {
	speaker.Lock()
	position := p.streamer.Position()
	speaker.Unlock()
	return p.format.SampleRate.D(position)
}

func (p *BeepPlayer) Duration() time.Duration {
	return p.format.SampleRate.D(p.streamer.Len())
}

func (p *BeepPlayer) Done() <-chan struct{} {
	return p.done
}

func (p *BeepPlayer) closeDone() {
	p.once.Do(func() {
		close(p.done)
	})
}

var speakerOnce sync.Once
var speakerErr error

func initSpeaker(sampleRate beep.SampleRate) error {
	speakerOnce.Do(func() {
		bufferSize := sampleRate.N(30 * time.Millisecond)
		speakerErr = speaker.Init(sampleRate, bufferSize)
	})
	if speakerErr != nil {
		return fmt.Errorf("initialize speaker: %w", speakerErr)
	}
	return nil
}
