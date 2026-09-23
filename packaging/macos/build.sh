#!/usr/bin/env bash
set -euo pipefail
gateway_root="$(cd "$(dirname "$0")/../.." && pwd)"
gateway_arch="${1:-arm64}"
gateway_version="${GATEWAY_VERSION:-0.1.0}"
gateway_stage="$(mktemp -d)"
trap 'rm -rf -- "$gateway_stage"' EXIT
gateway_app="$gateway_stage/SSH Gateway.app"
mkdir -p "$gateway_app/Contents/MacOS" "$gateway_app/Contents/Resources" "$gateway_root/dist"
GOARCH="$gateway_arch" CGO_ENABLED=1 go -C "$gateway_root/desktop" build -tags 'desktop,production' -trimpath -ldflags "-X ssh-gateway/updater.Version=$gateway_version -X ssh-gateway/updater.Repository=${GATEWAY_RELEASE_REPOSITORY:-}" -o "$gateway_app/Contents/MacOS/ssh-gateway-desktop" .
mkdir -p "$gateway_stage/icon.iconset"
for gateway_size in 16 32 128 256 512; do
 sips -z "$gateway_size" "$gateway_size" "$gateway_root/packaging/icon.png" --out "$gateway_stage/icon.iconset/icon_${gateway_size}x${gateway_size}.png" >/dev/null
 if [[ "$gateway_size" -le 256 ]]; then
  gateway_double=$((gateway_size * 2))
  sips -z "$gateway_double" "$gateway_double" "$gateway_root/packaging/icon.png" --out "$gateway_stage/icon.iconset/icon_${gateway_size}x${gateway_size}@2x.png" >/dev/null
 fi
done
iconutil -c icns "$gateway_stage/icon.iconset" -o "$gateway_app/Contents/Resources/icon.icns"
cat > "$gateway_app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>local.sshgateway.desktop</string>
<key>CFBundleName</key><string>SSH Gateway</string>
<key>CFBundleDisplayName</key><string>SSH Gateway</string>
<key>CFBundleExecutable</key><string>ssh-gateway-desktop</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleIconFile</key><string>icon.icns</string>
<key>CFBundleShortVersionString</key><string>$gateway_version</string>
<key>CFBundleVersion</key><string>$gateway_version</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
EOF
if [[ -n "${GATEWAY_MAC_SIGN_IDENTITY:-}" ]]; then
 codesign --force --options runtime --timestamp --sign "$GATEWAY_MAC_SIGN_IDENTITY" "$gateway_app"
else
 codesign --force --sign - "$gateway_app"
fi
ln -s /Applications "$gateway_stage/Applications"
ditto -c -k --sequesterRsrc --keepParent "$gateway_app" "$gateway_root/dist/ssh-gateway-desktop-${gateway_version}-macos-${gateway_arch}.app.zip"
gateway_dmg="$gateway_root/dist/ssh-gateway-desktop-${gateway_version}-macos-${gateway_arch}.dmg"
# DiskImages can briefly report Resource busy on hosted macOS runners.
for gateway_attempt in 1 2 3; do
 if hdiutil create -volname "SSH Gateway" -srcfolder "$gateway_stage" -ov -format UDZO "$gateway_dmg"; then
  hdiutil verify "$gateway_dmg"
  break
 fi
 if [[ "$gateway_attempt" -eq 3 ]]; then
  echo "Disk image creation failed after three attempts" >&2
  exit 1
 fi
 sleep "$((gateway_attempt * 5))"
done
