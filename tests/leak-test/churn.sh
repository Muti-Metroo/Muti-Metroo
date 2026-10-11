#!/usr/bin/env bash
# Forces peer reconnects by restarting exits in alternation. Each restart
# kills the h2 socket on transit -> transit detects via keepalive ->
# schedules reconnect -> re-dials -> the dial-side PeerConn.Close path that
# leaked pre-fix runs again. Without churn the leak has nothing to do.
#
# Usage: ./churn.sh <interval_seconds> <duration_seconds> <out_log>
set -uo pipefail

INTERVAL="${1:-30}"
DURATION="${2:-720}"
OUT_LOG="${3:-/tmp/churn.log}"

END_TS=$(( $(date +%s) + DURATION ))
TARGETS=(muti-metroo-leak-exit1 muti-metroo-leak-exit2)
i=0
cycles=0

: > "$OUT_LOG"
while [ "$(date +%s)" -lt "$END_TS" ]; do
  sleep "$INTERVAL"
  if [ "$(date +%s)" -ge "$END_TS" ]; then break; fi
  target="${TARGETS[$(( i % ${#TARGETS[@]} ))]}"
  i=$((i + 1))
  cycles=$((cycles + 1))
  ts=$(date -u +%H:%M:%S)
  printf '[%s] churn cycle %d: docker restart %s\n' "$ts" "$cycles" "$target" >> "$OUT_LOG"
  docker restart "$target" >> "$OUT_LOG" 2>&1 || true
done
echo "churn: total cycles=$cycles" >> "$OUT_LOG"
