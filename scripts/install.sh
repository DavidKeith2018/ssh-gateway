#!/usr/bin/env bash
set -euo pipefail

gateway_project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
gateway_bin_dir="${GATEWAY_BIN_DIR:-$HOME/.local/bin}"
gateway_data_dir="${GATEWAY_DATA_DIR:-$HOME/.local/share/ssh-gateway}"
gateway_unit_dir="${GATEWAY_UNIT_DIR:-$HOME/.config/systemd/user}"

if [[ ! -x "$gateway_project_dir/bin/ssh-gateway" ]]; then
  echo '请先执行 make build。' >&2
  exit 1
fi

mkdir -p -- "$gateway_bin_dir" "$gateway_unit_dir"
install -d -m 700 -- "$gateway_data_dir"
install -m 755 -- "$gateway_project_dir/bin/ssh-gateway" "$gateway_bin_dir/ssh-gateway"

# systemd 的引号与百分号转义，支持路径含空格。
gateway_quote() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//%/%%}"
  printf '"%s"' "$value"
}

{
  printf '[Unit]\nDescription=SSH Gateway\nAfter=network.target\n\n[Service]\nType=simple\n'
  printf 'ExecStart=%s -data %s -listen 127.0.0.1:2222 -web 127.0.0.1:8080 serve\n' "$(gateway_quote "$gateway_bin_dir/ssh-gateway")" "$(gateway_quote "$gateway_data_dir")"
  printf 'Restart=on-failure\nRestartSec=3\nUMask=0077\nNoNewPrivileges=yes\n\n[Install]\nWantedBy=default.target\n'
} > "$gateway_unit_dir/ssh-gateway.service"

printf '已安装：%s\n数据目录：%s\n服务配置：%s\n' "$gateway_bin_dir/ssh-gateway" "$gateway_data_dir" "$gateway_unit_dir/ssh-gateway.service"
printf '启动服务：systemctl --user daemon-reload && systemctl --user enable --now ssh-gateway\n'
printf '首次管理员密码：journalctl --user -u ssh-gateway -n 30\n'
