#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo "Usage: bash $0 UNIT NEW_BINARY /absolute/path/to/codebuddy-proxy [HEALTH_URL]" >&2
  exit 2
fi

unit=$1
new_binary=$(realpath -- "$2")
binary=$3
health_url=${4:-http://127.0.0.1:32126/health}
if [[ "$binary" != /* || ! -f "$binary" || ! -x "$binary" || -L "$binary" ]]; then
  echo "Provide the absolute path to the existing executable file." >&2
  exit 2
fi
binary=$(realpath -- "$binary")
if [[ ! -f "$new_binary" || ! -s "$new_binary" || "$new_binary" -ef "$binary" ]]; then
  echo "Provide a separate, nonempty replacement executable." >&2
  exit 2
fi
staged=
trap 'if [[ -n "$staged" ]]; then sudo rm -f -- "$staged"; fi' EXIT

running_binary() {
  local pid
  pid=$(systemctl show --property=MainPID --value -- "$unit") || return 1
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  sudo readlink -- "/proc/$pid/exe"
}

healthy() {
  local attempt
  for ((attempt=0; attempt<20; attempt++)); do
    if [[ "$(running_binary)" == "$binary" ]] &&
      curl -fsS --noproxy '*' --max-time 2 "$health_url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

sudo -v
exec 9<"$(dirname -- "$binary")"
flock -n 9
if [[ "$(running_binary)" != "$binary" ]]; then
  echo "The running executable for $unit does not match $binary." >&2
  exit 1
fi

backup=$(sudo mktemp -- "${binary}.backup.XXXXXXXX")
sudo cp -p -- "$binary" "$backup"
staged=$(sudo mktemp -- "${binary}.new.XXXXXXXX")
sudo cp -- "$new_binary" "$staged"
sudo chown --reference="$binary" -- "$staged"
sudo chmod --reference="$binary" -- "$staged"

if sudo systemctl stop -- "$unit" &&
  sudo mv -T -- "$staged" "$binary" &&
  sudo systemctl start -- "$unit" && healthy; then
  echo "Updated $unit. Previous executable: $backup"
  exit 0
fi

echo "Update failed. Restoring $backup." >&2
if sudo systemctl stop -- "$unit" &&
  sudo cp -p -- "$backup" "$staged" &&
  sudo mv -T -- "$staged" "$binary" &&
  sudo systemctl start -- "$unit" && healthy; then
  echo "Previous executable restored." >&2
else
  echo "Rollback could not be verified. Backup: $backup" >&2
fi
exit 1
