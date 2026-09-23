#!/usr/bin/env bash
set -euo pipefail

gateway_bin_dir="${GATEWAY_BIN_DIR:-$HOME/.local/bin}"
gateway_data_dir="${GATEWAY_DATA_DIR:-$HOME/.local/share/ssh-gateway}"
gateway_unit_dir="${GATEWAY_UNIT_DIR:-$HOME/.config/systemd/user}"

# 自定义目录用于离线安装与测试，不操作当前用户的 systemd 服务。
if [[ "$gateway_unit_dir" == "$HOME/.config/systemd/user" ]] && command -v systemctl >/dev/null; then
  systemctl --user disable --now ssh-gateway.service 2>/dev/null || true
fi
rm -f -- "$gateway_bin_dir/ssh-gateway" "$gateway_unit_dir/ssh-gateway.service"
if [[ "$gateway_unit_dir" == "$HOME/.config/systemd/user" ]] && command -v systemctl >/dev/null; then
  systemctl --user daemon-reload 2>/dev/null || true
fi
printf '程序和服务配置已卸载，数据仍保留在：%s\n' "$gateway_data_dir"
