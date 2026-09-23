#!/usr/bin/env bash
set -euo pipefail
gateway_root="$(cd "$(dirname "$0")/../.." && pwd)"
gateway_version="${GATEWAY_VERSION:-0.1.0}"
gateway_arch="${GATEWAY_ARCH:-amd64}"
case "$gateway_arch" in amd64|arm64) ;; *) echo 'Only amd64 and arm64 are supported' >&2; exit 1 ;; esac
gateway_stage="$(mktemp -d)"
trap 'rm -rf -- "$gateway_stage"' EXIT
gateway_name="ssh-gateway-server_${gateway_version}_linux_${gateway_arch}"
mkdir -p "$gateway_stage/$gateway_name" "$gateway_root/dist"
cd "$gateway_root"
CGO_ENABLED=0 GOOS=linux GOARCH="$gateway_arch" go build -trimpath -ldflags "-X ssh-gateway/updater.Version=$gateway_version -X ssh-gateway/updater.Repository=${GATEWAY_RELEASE_REPOSITORY:-}" -o "$gateway_stage/$gateway_name/ssh-gateway-server" ./cmd/ssh-gateway
install -m644 packaging/server/ssh-gateway-server.service packaging/server/ssh-gateway-server.conf SERVER.md "$gateway_stage/$gateway_name/"
tar -C "$gateway_stage" -czf "dist/$gateway_name.tar.gz" "$gateway_name"
sha256sum "dist/$gateway_name.tar.gz" > "dist/$gateway_name.tar.gz.sha256"
printf 'Package created: %s/dist/%s.tar.gz\n' "$gateway_root" "$gateway_name"
