#!/bin/bash
set -euo pipefail

APP_NAME="EspansoX"
BUNDLE_ID="com.espansox.EspansoX"
VERSION="1.0.0"

# Build release
swift build -c release

# Create app bundle structure
APP_DIR="${APP_NAME}.app/Contents"
mkdir -p "${APP_DIR}/MacOS"
mkdir -p "${APP_DIR}/Resources"

# Copy binary
cp ".build/release/${APP_NAME}" "${APP_DIR}/MacOS/${APP_NAME}"

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
