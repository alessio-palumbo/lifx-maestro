package analyzerbin

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnzipRestoresFilesModesAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions differ on windows")
	}

	archive := buildZip(t, func(w *zip.Writer) {
		addFile(t, w, "analyze/analyze", "binary", 0o755)
		addFile(t, w, "analyze/_internal/data.txt", "data", 0o644)
		addSymlink(t, w, "analyze/_internal/Python", "Python.framework/Python")
	})

	dest := t.TempDir()
	if err := unzip(bytes.NewReader(archive), int64(len(archive)), dest); err != nil {
		t.Fatalf("unzip: %v", err)
	}

	exePath := filepath.Join(dest, "analyze", "analyze")
	info, err := os.Lstat(exePath)
	if err != nil {
		t.Fatalf("stat executable: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit lost: mode %v", info.Mode())
	}

	linkPath := filepath.Join(dest, "analyze", "_internal", "Python")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "Python.framework/Python" {
		t.Fatalf("unexpected symlink target %q", target)
	}
}

func TestUnzipRejectsPathTraversal(t *testing.T) {
	archive := buildZip(t, func(w *zip.Writer) {
		addFile(t, w, "../escaped.txt", "nope", 0o644)
	})

	dest := t.TempDir()
	if err := unzip(bytes.NewReader(archive), int64(len(archive)), dest); err == nil {
		t.Fatal("expected traversal entry to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escaped.txt")); err == nil {
		t.Fatal("traversal entry escaped the destination directory")
	}
}

// The committed placeholder must not be mistaken for a real analyzer, otherwise
// development builds would skip the Python fallback.
func TestPlaceholderIsNotTreatedAsBundled(t *testing.T) {
	if len(analyzerZip) < placeholderMaxSize && Bundled() {
		t.Fatal("placeholder archive reported as bundled")
	}
	if !Bundled() {
		if _, err := EnsureInstalled(); err != ErrNotBundled {
			t.Fatalf("expected ErrNotBundled, got %v", err)
		}
	}
}

func TestNewAnalyzerFallsBackToPythonWhenNotBundled(t *testing.T) {
	if Bundled() {
		t.Skip("build carries a bundled analyzer")
	}

	analyzer, err := NewAnalyzer()
	if err != nil {
		t.Fatalf("NewAnalyzer: %v", err)
	}
	if analyzer.PythonPath == "" {
		t.Fatal("expected a python interpreter for the development fallback")
	}
	if !filepath.IsAbs(analyzer.ScriptPath) {
		t.Fatalf("script path must be absolute, got %q", analyzer.ScriptPath)
	}
	if _, err := os.Stat(analyzer.ScriptPath); err != nil {
		t.Fatalf("analyzer script not found: %v", err)
	}
}

func buildZip(t *testing.T, populate func(*zip.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	populate(w)
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func addFile(t *testing.T, w *zip.Writer, name, content string, mode os.FileMode) {
	t.Helper()
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	f, err := w.CreateHeader(header)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func addSymlink(t *testing.T, w *zip.Writer, name, target string) {
	t.Helper()
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	f, err := w.CreateHeader(header)
	if err != nil {
		t.Fatalf("create symlink %s: %v", name, err)
	}
	if _, err := f.Write([]byte(target)); err != nil {
		t.Fatalf("write symlink %s: %v", name, err)
	}
}
