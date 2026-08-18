// Package analyzerbin ships the Python audio analyzer as a self-contained
// executable so released builds analyze audio without a Python install.
//
// Release CI freezes analyzer/analyze.py with PyInstaller and overwrites
// assets/analyzer.zip with the resulting onedir bundle. Development builds keep
// the committed placeholder, which reports ErrNotBundled so callers fall back to
// a local Python interpreter plus analyzer/analyze.py.
package analyzerbin

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	_ "embed"
)

//go:embed assets/analyzer.zip
var analyzerZip []byte

// ErrNotBundled reports that this build carries the placeholder archive rather
// than a frozen analyzer.
var ErrNotBundled = errors.New("no analyzer bundled in this build")

// placeholderMaxSize is larger than an empty zip's end-of-central-directory
// record and far smaller than a real frozen analyzer.
const placeholderMaxSize = 1024

var (
	once        sync.Once
	cachedPath  string
	cachedError error
)

// Bundled reports whether this build carries a frozen analyzer.
func Bundled() bool {
	return len(analyzerZip) >= placeholderMaxSize
}

// EnsureInstalled extracts the bundled analyzer on first use and returns the
// path to its executable. Repeat calls reuse the already-installed copy.
// It returns ErrNotBundled for development builds.
func EnsureInstalled() (string, error) {
	once.Do(func() {
		cachedPath, cachedError = install()
	})
	return cachedPath, cachedError
}

func install() (string, error) {
	if !Bundled() {
		return "", ErrNotBundled
	}

	baseDir, err := installDir()
	if err != nil {
		return "", err
	}
	exePath := executablePath(baseDir)
	versionFile := filepath.Join(baseDir, ".analyzer-version")

	// Reuse the installed copy only when it matches this build and survived a
	// previous extraction intact.
	if data, err := os.ReadFile(versionFile); err == nil && strings.TrimSpace(string(data)) == Version {
		if _, err := os.Stat(exePath); err == nil {
			return exePath, nil
		}
	}

	// Drop a stale or half-written install before laying down the new one.
	if err := os.RemoveAll(filepath.Join(baseDir, analyzerDirName)); err != nil {
		return "", fmt.Errorf("remove previous analyzer: %w", err)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", fmt.Errorf("create analyzer dir: %w", err)
	}
	if err := unzip(bytes.NewReader(analyzerZip), int64(len(analyzerZip)), baseDir); err != nil {
		return "", fmt.Errorf("extract analyzer: %w", err)
	}
	if _, err := os.Stat(exePath); err != nil {
		return "", fmt.Errorf("bundled analyzer missing executable %s: %w", exePath, err)
	}
	if runtime.GOOS != "windows" {
		// Zips built on Windows lose the executable bit.
		if err := os.Chmod(exePath, 0o755); err != nil {
			return "", fmt.Errorf("mark analyzer executable: %w", err)
		}
	}

	// Stamp last so an interrupted extraction is retried rather than trusted.
	if err := os.WriteFile(versionFile, []byte(Version), 0o644); err != nil {
		return "", fmt.Errorf("record analyzer version: %w", err)
	}
	return exePath, nil
}

// analyzerDirName is the onedir folder PyInstaller produces for analyze.py.
const analyzerDirName = "analyze"

func installDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".lifx-maestro", "analyzer"), nil
}

func executablePath(baseDir string) string {
	name := analyzerDirName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(baseDir, analyzerDirName, name)
}

func unzip(data io.ReaderAt, size int64, dest string) error {
	r, err := zip.NewReader(data, size)
	if err != nil {
		return err
	}
	for _, f := range r.File {
		fpath, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, fpath); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, fpath string) error {
	// PyInstaller bundles contain symlinks inside framework directories.
	if f.Mode()&os.ModeSymlink != 0 {
		return writeSymlink(f, fpath)
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeSymlink(f *zip.File, fpath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	target, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	if err := os.Remove(fpath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(string(target), fpath)
}

// safeJoin rejects archive entries that would escape the destination directory.
func safeJoin(dest, name string) (string, error) {
	fpath := filepath.Join(dest, name)
	if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal analyzer archive path %q", name)
	}
	return fpath, nil
}
