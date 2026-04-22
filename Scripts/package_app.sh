#!/bin/bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

APP_NAME="Sokki"
BUNDLE_ID="com.sokki.Sokki"
VERSION="${VERSION:-$(cat VERSION)}"
SHORT_VERSION="${VERSION}"
BUILD_DIR=".build/release"
APP_BUNDLE="${APP_NAME}.app"
CONTENTS="${APP_BUNDLE}/Contents"
FRAMEWORKS="${CONTENTS}/Frameworks"
SIGN_IDENTITY="Developer ID Application: chen cheng (KU8S35TEW8)"

# ---- Sparkle config: single source of truth for public key + feed URL ----
SPARKLE_CONFIG="Scripts/sparkle.config"
if [[ ! -f "${SPARKLE_CONFIG}" ]]; then
    echo "FAIL: ${SPARKLE_CONFIG} not found." >&2
    exit 1
fi
# shellcheck disable=SC1090
source "${SPARKLE_CONFIG}"

if [[ -z "${SPARKLE_PUBLIC_KEY:-}" || -z "${SPARKLE_FEED_URL:-}" ]]; then
    echo "FAIL: ${SPARKLE_CONFIG} is missing SPARKLE_PUBLIC_KEY or SPARKLE_FEED_URL." >&2
    echo "      See docs/designs/2026-04-19-sparkle-r2-autoupdate.md §3a for one-time setup." >&2
    exit 1
fi

# Validate Ed25519 public key shape: 32 bytes -> 44 chars base64 ending with '='.
if ! [[ "${SPARKLE_PUBLIC_KEY}" =~ ^[A-Za-z0-9+/]{43}=$ ]]; then
    echo "FAIL: SPARKLE_PUBLIC_KEY in ${SPARKLE_CONFIG} doesn't look like a valid Ed25519 base64 key." >&2
    echo "      Expected 44 chars ending with '='. Got: '${SPARKLE_PUBLIC_KEY}'" >&2
    exit 1
fi

# ---- Telemetry config (optional): endpoint URL + HMAC key ----
# If Scripts/telemetry.config exists, its values get baked into Info.plist so
# CrashReporter / TelemetryService can activate. Absent config → services stay
# silent (matches the SUFeedURL gate pattern).
TELEMETRY_CONFIG="Scripts/telemetry.config"
if [[ -f "${TELEMETRY_CONFIG}" ]]; then
    # shellcheck disable=SC1090
    source "${TELEMETRY_CONFIG}"
fi

if [[ -n "${TELEMETRY_ENDPOINT_URL:-}" && -n "${TELEMETRY_INGEST_KEY:-}" ]]; then
    TELEMETRY_PLIST_KEYS="    <key>TelemetryEndpointURL</key>
    <string>${TELEMETRY_ENDPOINT_URL}</string>
    <key>TelemetryIngestKey</key>
    <string>${TELEMETRY_INGEST_KEY}</string>"
    echo "Telemetry endpoint: ${TELEMETRY_ENDPOINT_URL}"
else
    TELEMETRY_PLIST_KEYS=""
    echo "Telemetry: disabled (no Scripts/telemetry.config or env vars)"
fi

echo "Building ${APP_NAME} ${VERSION} (release)..."
swift build -c release

echo "Creating app bundle..."
rm -rf "${APP_BUNDLE}"
mkdir -p "${CONTENTS}/MacOS"
mkdir -p "${CONTENTS}/Resources"
mkdir -p "${FRAMEWORKS}"

cp "${BUILD_DIR}/${APP_NAME}" "${CONTENTS}/MacOS/${APP_NAME}"
cp "Resources/AppIcon.icns" "${CONTENTS}/Resources/AppIcon.icns"

# swift build produces a binary without @executable_path/../Frameworks in LC_RPATH.
# Xcode adds this automatically; SPM + hand-rolled bundling does not. Without it,
# dyld looks for Sparkle.framework in the wrong path and the app crashes at launch
# with "Library not loaded". Patch the Mach-O BEFORE signing — modifying rpath
# invalidates any prior signature.
echo "Patching rpath so dyld finds Contents/Frameworks/..."
install_name_tool -add_rpath "@executable_path/../Frameworks" \
                  "${CONTENTS}/MacOS/${APP_NAME}"

# ---- Embed Sparkle.framework ----
# Sparkle ships as an XCFramework via SPM binaryTarget. Pin the macos-arm64 slice
# explicitly so a future x86_64-only slice can't sort first and get picked silently.
SPARKLE_XCFRAMEWORK=$(find .build/artifacts -type d -name "Sparkle.xcframework" | head -n 1)
if [[ -z "${SPARKLE_XCFRAMEWORK}" ]]; then
    echo "FAIL: Sparkle.xcframework not found under .build/artifacts." >&2
    echo "      Did 'swift build -c release' complete successfully?" >&2
    exit 1
fi
SPARKLE_FRAMEWORK=$(find "${SPARKLE_XCFRAMEWORK}" -type d -path "*macos-arm64*" -name "Sparkle.framework" | head -n 1)
if [[ -z "${SPARKLE_FRAMEWORK}" ]]; then
    echo "FAIL: Sparkle.framework (macos-arm64 slice) not found inside ${SPARKLE_XCFRAMEWORK}." >&2
    exit 1
fi
echo "Embedding Sparkle from ${SPARKLE_FRAMEWORK}..."
cp -R "${SPARKLE_FRAMEWORK}" "${FRAMEWORKS}/"

cat > "${CONTENTS}/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleDisplayName</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIdentifier</key>
    <string>${BUNDLE_ID}</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${SHORT_VERSION}</string>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>NSHighResolutionCapable</key>
    <true/>
    <key>SUFeedURL</key>
    <string>${SPARKLE_FEED_URL}</string>
    <key>SUPublicEDKey</key>
    <string>${SPARKLE_PUBLIC_KEY}</string>
    <key>SUEnableAutomaticChecks</key>
    <true/>
    <key>SUScheduledCheckInterval</key>
    <integer>86400</integer>
    <key>SUAllowsAutomaticUpdates</key>
    <true/>
    <key>SUEnableSystemProfiling</key>
    <false/>
${TELEMETRY_PLIST_KEYS}
</dict>
</plist>
PLIST

# ---- Code sign: inside-out, hardened runtime ----
# Sparkle.framework contains nested XPC services and helpers that must be signed first.
# Re-sign them with our Developer ID so notarization accepts the whole bundle under our team.
EMBEDDED_SPARKLE="${FRAMEWORKS}/Sparkle.framework"
echo "Code signing Sparkle helpers (inside-out)..."

# Deepest bundles first (xpc, app), then the framework itself. Null-delimited for safety.
while IFS= read -r -d '' bundle; do
    codesign --force --options runtime --timestamp \
             --sign "${SIGN_IDENTITY}" \
             "${bundle}"
done < <(find "${EMBEDDED_SPARKLE}" \
            \( -name "*.xpc" -o -name "*.app" \) \
            -type d -print0 \
            | sort -rz)

# Sign any raw helper executables inside Versions/*/ (e.g., Autoupdate).
for helper in "${EMBEDDED_SPARKLE}/Versions/"*/Autoupdate; do
    [[ -f "${helper}" ]] || continue
    codesign --force --options runtime --timestamp \
             --sign "${SIGN_IDENTITY}" \
             "${helper}"
done

echo "Code signing Sparkle.framework..."
codesign --force --options runtime --timestamp \
         --sign "${SIGN_IDENTITY}" \
         "${EMBEDDED_SPARKLE}"

echo "Code signing main app with Developer ID + hardened runtime..."
codesign --force --options runtime --timestamp \
         --sign "${SIGN_IDENTITY}" \
         "${APP_BUNDLE}"

echo "Verifying signature..."
codesign --verify --strict --verbose=2 "${APP_BUNDLE}"

echo "Done! ${APP_BUNDLE} is signed (not yet notarized)."
