// Package python provides an optional persistent analyzer process. It does not
// extract bundled assets or select a Python installation.
package python

import (
	"context"
	"fmt"
	"github.com/alessio-palumbo/lifx-maestro/internal/analysis"
	core "github.com/alessio-palumbo/lifx-maestro/internal/live"
	"github.com/alessio-palumbo/lifx-maestro/live"
	"path/filepath"
)

type Config struct {
	BinaryPath string
	PythonPath string
	ScriptPath string
}

// New starts either BinaryPath --live or PythonPath ScriptPath --live.
// Use absolute runtime paths. The caller must Close the returned analyzer after
// Engine.Run has returned. Canceling ctx terminates the process.
func New(ctx context.Context, config Config) (live.Analyzer, error) {
	for _, path := range []string{config.BinaryPath, config.PythonPath, config.ScriptPath} {
		if path != "" && !filepath.IsAbs(path) {
			return nil, fmt.Errorf("live analyzer runtime paths must be absolute: %q", path)
		}
	}
	return core.NewPythonAnalyzer(ctx, analysis.Analyzer{
		BinaryPath: config.BinaryPath, PythonPath: config.PythonPath, ScriptPath: config.ScriptPath,
	})
}
