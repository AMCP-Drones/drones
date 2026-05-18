#!/usr/bin/env bash
# Run all regulator security-goal checks (local dry-run).
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
for s in fw_sec_01.sh fw_sec_02.sh fw_sec_03.sh fw_sec_05.sh sys_sec_01.sh; do
  echo "==> $s"
  bash "$DIR/$s"
done
echo "All regulator goal scripts passed."
