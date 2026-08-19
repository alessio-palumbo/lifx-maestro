#!/usr/bin/env bash
#
# Freeze the Python analyzer and embed it, reproducing what release CI does so a
# packaged build can be tested locally.
#
# Doing this by hand is error prone: skipping the final zip leaves the committed
# placeholder in place, and the resulting app silently falls back to a local Python
# interpreter. It then behaves like a development build — no first-run walkthrough,
# analysis working only because a venv happens to be present — which looks like
# success while testing none of the bundled path.
#
# Usage:
#   scripts/bundle-analyzer.sh          # freeze, verify, embed
#   wails build                         # then build the app
#   git checkout internal/analyzerbin/assets/analyzer.zip   # restore placeholder
set -euo pipefail

cd "$(dirname "$0")/.."

PYTHON="${PYTHON:-analyzer/.venv/bin/python}"
ASSET="internal/analyzerbin/assets/analyzer.zip"
DIST="analyzer-dist"
WORK="analyzer-build"

if [ ! -x "$PYTHON" ]; then
  echo "error: $PYTHON not found. Create it with:" >&2
  echo "  python3 -m venv analyzer/.venv" >&2
  echo "  analyzer/.venv/bin/python -m pip install -r analyzer/requirements.txt pyinstaller" >&2
  exit 1
fi

echo "==> Freezing analyzer"
"$PYTHON" -m PyInstaller --noconfirm --clean --onedir --name analyze \
  --distpath "$DIST" --workpath "$WORK" --specpath "$WORK" \
  --collect-all librosa \
  --collect-all lazy_loader \
  --collect-all soxr \
  --collect-all soundfile \
  --collect-all audioread \
  --collect-all numba \
  --collect-all llvmlite \
  --collect-submodules sklearn \
  analyzer/analyze.py >/dev/null

# A frozen bundle can build cleanly and still fail on import, so run it once.
echo "==> Smoke testing"
"$PYTHON" - <<'PY'
import numpy as np, soundfile as sf
sr = 44100
t = np.linspace(0, 3, sr * 3, endpoint=False)
sf.write("analyzer-build/smoke.wav", 0.2 * np.sin(2 * np.pi * 220 * t), sr)
PY
"$DIST/analyze/analyze" "$WORK/smoke.wav" | "$PYTHON" -c 'import json,sys; json.load(sys.stdin)'

echo "==> Embedding into $ASSET"
rm -f "$ASSET"
# -y preserves the symlinks PyInstaller creates inside Python.framework.
(cd "$DIST" && zip -qry "../$ASSET" analyze)

size=$(wc -c <"$ASSET")
if [ "$size" -lt 1048576 ]; then
  echo "error: $ASSET is only $size bytes; the bundle did not embed" >&2
  exit 1
fi

cat <<EOF

Embedded $(( size / 1024 / 1024 )) MB analyzer bundle.

Next:
  wails build                                            # package the app
  git checkout $ASSET   # restore the placeholder when done
EOF
