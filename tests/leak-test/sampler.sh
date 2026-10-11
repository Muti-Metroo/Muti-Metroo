#!/usr/bin/env bash
# Periodically samples per-container peer/socket metrics. Output is a
# CSV-style log read by analyze.sh at the end.
#
# Mesh port (h2 listeners on transit/exit1/exit2): 8443 = 0x20FB.
# We count TCP sockets in ESTABLISHED state (st = 01 hex) whose local OR
# remote port is 8443 across IPv4 and IPv6. That isolates inter-agent
# sockets and excludes the HTTP API socket / nginx target sockets.
#
# Each line of the output log:
#   ts,container,peer_count,mesh_sockets
#
# Usage: ./sampler.sh <interval_seconds> <duration_seconds> <out_log>
set -uo pipefail

INTERVAL="${1:-30}"
DURATION="${2:-720}"
OUT_LOG="${3:-/tmp/sampler.log}"

CONTAINERS=(muti-metroo-leak-ingress muti-metroo-leak-transit muti-metroo-leak-exit1 muti-metroo-leak-exit2)
HEALTH_PORTS=(18081 18082 18083 18084)

END_TS=$(( $(date +%s) + DURATION ))

# AWK that prints the ESTABLISHED-mesh-socket count from /proc/net/tcp*.
# Reads stdin from `cat /proc/net/tcp /proc/net/tcp6`. State col is $4,
# local addr col is $2, remote addr col is $3. Skips the header line.
AWK_PROG='
NR == 1 || $1 == "sl" { next }
$4 == "01" && ($2 ~ /:20FB$/ || $3 ~ /:20FB$/) { n++ }
END { print n + 0 }
'

: > "$OUT_LOG"
echo "ts,container,peer_count,mesh_sockets" >> "$OUT_LOG"

while [ "$(date +%s)" -lt "$END_TS" ]; do
  ts=$(date -u +%H:%M:%S)
  for idx in "${!CONTAINERS[@]}"; do
    c="${CONTAINERS[$idx]}"
    p="${HEALTH_PORTS[$idx]}"

    # peer_count from /healthz (best-effort; container may be restarting).
    # Use a targeted regex grep -- the JSON has many bool fields, so simple
    # field splitting picks up the wrong value.
    pc=$(curl --silent --max-time 2 "http://127.0.0.1:${p}/healthz" 2>/dev/null \
           | grep -oE '"peer_count":[0-9]+' | grep -oE '[0-9]+$')
    pc="${pc:-NA}"

    # mesh_sockets = TCP-ESTABLISHED rows in container netns touching :8443
    ms=$(docker exec "$c" sh -c 'cat /proc/net/tcp /proc/net/tcp6 2>/dev/null' 2>/dev/null \
           | awk "$AWK_PROG")
    ms="${ms:-NA}"

    echo "${ts},${c},${pc},${ms}" >> "$OUT_LOG"
  done
  sleep "$INTERVAL"
done
