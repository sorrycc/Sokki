#!/bin/bash
set -euo pipefail

APP_NAME="EspansoX"
BUNDLE_ID="com.espansox.EspansoX"
VERSION="1.0.0"
ICON_SOURCE="tmp/espansox-logo-05-key-wand.png"

# Build release
swift build -c release

# Create app bundle structure
APP_DIR="${APP_NAME}.app/Contents"
mkdir -p "${APP_DIR}/MacOS"
mkdir -p "${APP_DIR}/Resources"

# Copy binary
cp ".build/release/${APP_NAME}" "${APP_DIR}/MacOS/${APP_NAME}"

# Generate .icns from PNG
ICONSET_DIR=$(mktemp -d)/AppIcon.iconset
mkdir -p "${ICONSET_DIR}"
for size in 16 32 128 256 512; do
    sips -z $size $size "${ICON_SOURCE}" --out "${ICONSET_DIR}/icon_${size}x${size}.png" >/dev/null
    double=$((size * 2))
    sips -z $double $double "${ICON_SOURCE}" --out "${ICONSET_DIR}/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "${ICONSET_DIR}" -o "${APP_DIR}/Resources/AppIcon.icns"
rm -rf "$(dirname "${ICONSET_DIR}")"

# Generate Info.plist
cat > "${APP_DIR}/Info.plist" << EOF
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
    <string>${VERSION}</string>
    <key>CFBundleExecutable</key>
    <string>${APP_NAME}</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSMinimumSystemVersion</key>
    <string>14.0</string>
    <key>LSUIElement</key>
    <true/>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
EOF

# Code sign
codesign --force --sign - "${APP_NAME}.app"

echo "Built ${APP_NAME}.app successfully"
echo "Run: open ${APP_NAME}.app"
