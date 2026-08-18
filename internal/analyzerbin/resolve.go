package analyzerbin

import (
	"os"
	"path/filepath"
	"runtime"

	"lifx-maestro/internal/analysis"
)

// NewAnalyzer returns an analyzer for the current build: the bundled
// self-contained executable in released builds, or a local Python interpreter
// plus analyzer/analyze.py during development.
//
// Both paths are resolved absolutely. Relative paths are unusable from a macOS
// .app or Windows install, where the working directory is not the repo root.
func NewAnalyzer() (analysis.Analyzer, error) {
	analyzer := analysis.NewAnalyzer()

	if Bundled() {
		exePath, err := EnsureInstalled()
		if err != nil {
			return analyzer, err
		}
		analyzer.BinaryPath = exePath
		return analyzer, nil
	}

	analyzer.PythonPath = DevPythonPath()
	analyzer.ScriptPath = DevScriptPath()
	return analyzer, nil
}

// DevPythonPath returns the interpreter to use for development builds,
// preferring a project virtualenv over whatever is on PATH.
func DevPythonPath() string {
	relative := []string{
		filepath.Join("analyzer", ".venv", "bin", "python"),
		filepath.Join(".venv", "bin", "python"),
	}
	if runtime.GOOS == "windows" {
		relative = []string{
			filepath.Join("analyzer", ".venv", "Scripts", "python.exe"),
			filepath.Join(".venv", "Scripts", "python.exe"),
		}
	}

	// Prefer a venv under the source tree, then one under the working directory.
	root := repoRoot()
	for _, candidate := range relative {
		if root != "" {
			if absolute := filepath.Join(root, candidate); exists(absolute) {
				return absolute
			}
		}
		if absolute, err := filepath.Abs(candidate); err == nil && exists(absolute) {
			return absolute
		}
	}

	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// DevScriptPath returns the absolute path to analyzer/analyze.py in the source tree.
func DevScriptPath() string {
	if root := repoRoot(); root != "" {
		return filepath.Join(root, "analyzer", "analyze.py")
	}
	absolute, err := filepath.Abs(filepath.Join("analyzer", "analyze.py"))
	if err != nil {
		return filepath.Join("analyzer", "analyze.py")
	}
	return absolute
}

// repoRoot locates the source tree from this file's compile-time path. It
// resolves to a real directory only on the machine that built the binary, which
// is exactly the development case this fallback serves.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	if !exists(filepath.Join(root, "analyzer", "analyze.py")) {
		return ""
	}
	return filepath.Clean(root)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
