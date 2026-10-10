#!/usr/bin/env bash
# Concurrent SOCKS5 load generator. Mirrors "10 TUN users continuously
# pushing traffic" -- each worker hammers the target HTTP server through
# the ingress agent's SOCKS5 listener.
#
# Usage: ./loadgen.sh <workers> <duration_seconds> <out_log>
set -uo pipefail

WORKERS="${1:-10}"
DURATION="${2:-720}"
OUT_LOG="${3:-/tmp/loadgen.log}"

SOCKS_HOST="127.0.0.1:11080"
TARGET_URL="http://172.29.0.100/"
END_TS=$(( $(date +%s) + DURATION ))

worker() {
  local id="$1"
  local ok=0 fail=0
  while [ "$(date +%s)" -lt "$END_TS" ]; do
    if curl --silent --show-error --max-time 10 \
            --socks5-hostname "$SOCKS_HOST" \
            -o /dev/null -w '' "$TARGET_URL" 2>/dev/null; then
      ok=$((ok + 1))
    else
      fail=$((fail + 1))
    fi
    # Tiny pause so we generate steady load instead of a tight CPU loop.
    sleep 0.1
  done
  printf 'worker=%d ok=%d fail=%d\n' "$id" "$ok" "$fail" >> "$OUT_LOG"
}

: > "$OUT_LOG"
for i in $(seq 1 "$WORKERS"); do
  worker "$i" &
done
wait
echo "loadgen: all workers exited" >> "$OUT_LOG"
