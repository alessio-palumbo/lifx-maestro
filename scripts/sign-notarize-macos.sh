#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 2 ]]; then
  echo "usage: $0 <app-path> <archive-path>" >&2
  exit 2
fi

APP_PATH="$1"
ARCHIVE_PATH="$2"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTITLEMENTS_PATH="${MACOS_ENTITLEMENTS_PATH:-$SCRIPT_DIR/../build/darwin/entitlements.plist}"

: "${APPLE_ID:?APPLE_ID is required}"
: "${APPLE_TEAM_ID:?APPLE_TEAM_ID is required}"
: "${APPLE_APP_SPECIFIC_PASSWORD:?APPLE_APP_SPECIFIC_PASSWORD is required}"

if [[ ! -d "$APP_PATH" ]]; then
  echo "error: app bundle not found: $APP_PATH" >&2
  exit 1
fi
if [[ ! -f "$ENTITLEMENTS_PATH" ]]; then
  echo "error: entitlements not found: $ENTITLEMENTS_PATH" >&2
  exit 1
fi

SIGNING_IDENTITY="${MACOS_SIGNING_IDENTITY:-}"
if [[ -z "$SIGNING_IDENTITY" ]]; then
  SIGNING_IDENTITY="$({ security find-identity -v -p codesigning || true; } \
    | sed -n 's/.*"\(Developer ID Application:.*\)"/\1/p' \
    | head -n 1)"
fi
if [[ -z "$SIGNING_IDENTITY" ]]; then
  echo "error: no Developer ID Application identity is available" >&2
  exit 1
fi

echo "Signing Mach-O files with $SIGNING_IDENTITY"
while IFS= read -r -d '' candidate; do
  if file -b "$candidate" | grep -q 'Mach-O'; then
    codesign --force --options runtime --timestamp \
      --sign "$SIGNING_IDENTITY" "$candidate"
  fi
done < <(find "$APP_PATH/Contents" -type f -print0)

codesign --force --options runtime --timestamp \
  --entitlements "$ENTITLEMENTS_PATH" \
  --sign "$SIGNING_IDENTITY" "$APP_PATH"
codesign --verify --deep --strict --verbose=2 "$APP_PATH"
if ! codesign -d --entitlements :- "$APP_PATH" 2>&1 \
  | grep -q 'com.apple.security.device.audio-input'; then
  echo "error: signed app is missing the audio-input entitlement" >&2
  exit 1
fi

mkdir -p "$(dirname "$ARCHIVE_PATH")"
rm -f "$ARCHIVE_PATH"
ditto -c -k --keepParent "$APP_PATH" "$ARCHIVE_PATH"

# Verify packaging preserves the signature before submitting the archive.
VERIFY_DIR="$(mktemp -d)"
trap 'rm -rf "$VERIFY_DIR"' EXIT
ditto -x -k "$ARCHIVE_PATH" "$VERIFY_DIR/pre-notarization"
codesign --verify --deep --strict --verbose=2 \
  "$VERIFY_DIR/pre-notarization/$(basename "$APP_PATH")"

echo "Submitting $ARCHIVE_PATH for notarization"
xcrun notarytool submit "$ARCHIVE_PATH" \
  --apple-id "$APPLE_ID" \
  --team-id "$APPLE_TEAM_ID" \
  --password "$APPLE_APP_SPECIFIC_PASSWORD" \
  --wait

# ZIP archives cannot be stapled, so staple the ticket to the app and then
# recreate the final ZIP with the stapled app inside it.
xcrun stapler staple "$APP_PATH"
xcrun stapler validate "$APP_PATH"
codesign --verify --deep --strict --verbose=2 "$APP_PATH"
spctl --assess --type execute --verbose=4 "$APP_PATH"

rm -f "$ARCHIVE_PATH"
ditto -c -k --keepParent "$APP_PATH" "$ARCHIVE_PATH"
ditto -x -k "$ARCHIVE_PATH" "$VERIFY_DIR/final"
codesign --verify --deep --strict --verbose=2 \
  "$VERIFY_DIR/final/$(basename "$APP_PATH")"
