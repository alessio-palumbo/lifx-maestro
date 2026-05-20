package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ValidateInput(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("audio path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat audio file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("audio path %q is a directory", path)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".wav", ".flac", ".ogg", ".m4a", ".aac":
		return nil
	default:
		return fmt.Errorf("unsupported audio extension %q", filepath.Ext(path))
	}
}

func TimelineName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		return "generated"
	}
	return name
}

func DefaultTimelinePath(path string) string {
	return filepath.Join("projects", TimelineName(path)+".json")
}
