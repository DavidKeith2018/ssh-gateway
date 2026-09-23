#!/usr/bin/env bash
set -euo pipefail
gateway_root="$(cd "$(dirname "$0")/../.." && pwd)"
gateway_version="${GATEWAY_VERSION:-0.1.0}"
gateway_binary="${GATEWAY_DESKTOP_BINARY:-$gateway_root/dist/ssh-gateway-desktop}"
gateway_package="$(mktemp -d)"
trap 'rm -rf -- "$gateway_package"' EXIT
mkdir -p "$gateway_package/DEBIAN" "$gateway_package/usr/bin" "$gateway_package/usr/share/applications" "$gateway_package/usr/share/icons/hicolor/scalable/apps" "$gateway_root/dist"
install -m755 "$gateway_binary" "$gateway_package/usr/bin/ssh-gateway-desktop"
install -m644 "$gateway_root/packaging/icon.svg" "$gateway_package/usr/share/icons/hicolor/scalable/apps/ssh-gateway.svg"
cat > "$gateway_package/DEBIAN/control" <<EOF
Package: ssh-gateway-desktop
Version: $gateway_version
Section: net
Priority: optional
Architecture: amd64
Maintainer: SSH Gateway
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Description: Desktop SSH gateway
 Separate SSH credentials, source restrictions, and desktop terminals.
EOF
cat > "$gateway_package/usr/share/applications/ssh-gateway.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=SSH Gateway
Comment=Manage SSH gateway connections
Exec=ssh-gateway-desktop
Icon=ssh-gateway
Terminal=false
Categories=Network;Utility;
StartupWMClass=ssh-gateway-desktop
EOF
dpkg-deb --root-owner-group --build "$gateway_package" "$gateway_root/dist/ssh-gateway-desktop_${gateway_version}_amd64.deb"
