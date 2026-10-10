#!/usr/bin/env bash
# Orchestrates the leak verification end-to-end:
#   1. Reset state dirs and bring up the 4-agent + target mesh.
#   2. Wait for routes to propagate.
#   3. Start load generator (10 SOCKS5 workers), churn driver (alternate
#      exit restarts every 30s), and metrics sampler (every 15s).
#   4. Run for DURATION seconds (default 720 = 12 min).
#   5. Stop drivers, analyze sampler log, print verdict, optionally teardown.
#
# Flags:
#   --no-build       Skip docker compose build
#   --no-teardown    Leave containers running after the test
#   --duration N     Seconds to run load + churn + sampling (default 720)
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
cd "$HERE"

DURATION=720
DO_BUILD=true
DO_TEARDOWN=true
WORKERS=10
CHURN_INTERVAL=30
SAMPLE_INTERVAL=15

while [ $# -gt 0 ]; do
  case "$1" in
    --no-build)    DO_BUILD=false ;;
    --no-teardown) DO_TEARDOWN=false ;;
    --duration)    DURATION="$2"; shift ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
  shift
done

LOG_DIR="$HERE/logs"
mkdir -p "$LOG_DIR"
LOADGEN_LOG="$LOG_DIR/loadgen.log"
CHURN_LOG="$LOG_DIR/churn.log"
SAMPLER_LOG="$LOG_DIR/sampler.log"
RUN_LOG="$LOG_DIR/run.log"

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "$RUN_LOG"; }

: > "$RUN_LOG"
log "leak-test: WORKERS=$WORKERS CHURN_INTERVAL=${CHURN_INTERVAL}s SAMPLE_INTERVAL=${SAMPLE_INTERVAL}s DURATION=${DURATION}s"

# Reset any leftover state from previous runs.
for a in ingress transit exit1 exit2; do
  rm -rf "$HERE/$a/data"/* 2>/dev/null || true
  mkdir -p "$HERE/$a/data"
done

# Down first to clear any leftover container state and recreate the network.
docker compose -p muti-metrooleak down --remove-orphans >/dev/null 2>&1 || true

if $DO_BUILD; then
  log "building agent image..."
  docker compose -p muti-metrooleak build >>"$RUN_LOG" 2>&1
fi

log "starting mesh..."
docker compose -p muti-metrooleak up -d >>"$RUN_LOG" 2>&1

# Wait for all agents to answer /healthz.
log "waiting for agents to be ready..."
for port in 18081 18082 18083 18084; do
  for _ in $(seq 1 60); do
    if curl --silent --max-time 1 "http://127.0.0.1:${port}/healthz" >/dev/null; then
      break
    fi
    sleep 1
  done
done

# Wait for ingress to actually see a route to 172.29.0.100 (the target).
log "waiting for route to target to propagate..."
for _ in $(seq 1 60); do
  resp=$(curl --silent --max-time 1 "http://127.0.0.1:18081/healthz" \
           | grep -oE '"route_count":[0-9]+' | grep -oE '[0-9]+$')
  if [ -n "${resp:-}" ] && [ "$resp" -ge 1 ]; then
    log "ingress route_count=$resp"
    break
  fi
  sleep 1
done

# Smoke-test one SOCKS request before unleashing the load.
log "smoke test: one request through SOCKS5..."
if ! curl --silent --max-time 10 --socks5-hostname 127.0.0.1:11080 \
        -o /dev/null -w 'smoke http_code=%{http_code}\n' \
        http://172.29.0.100/ | tee -a "$RUN_LOG"; then
  log "ERROR: smoke test failed; aborting"
  $DO_TEARDOWN && docker compose -p muti-metrooleak logs --tail=200 >>"$RUN_LOG" 2>&1
  exit 1
fi

EXPECTED_CYCLES=$(( DURATION / CHURN_INTERVAL - 1 ))
log "starting drivers: loadgen ($WORKERS workers), churn (~$EXPECTED_CYCLES cycles), sampler (every ${SAMPLE_INTERVAL}s)"

./loadgen.sh "$WORKERS" "$DURATION" "$LOADGEN_LOG" &
LOADGEN_PID=$!
./churn.sh "$CHURN_INTERVAL" "$DURATION" "$CHURN_LOG" &
CHURN_PID=$!
./sampler.sh "$SAMPLE_INTERVAL" "$DURATION" "$SAMPLER_LOG" &
SAMPLER_PID=$!

# Wait for the drivers to finish.
wait "$LOADGEN_PID" 2>/dev/null || true
wait "$CHURN_PID" 2>/dev/null || true
wait "$SAMPLER_PID" 2>/dev/null || true
log "drivers finished"

# Loadgen summary.
log "loadgen summary:"
awk '
  /^worker=/ { ok += $2 + 0; sub(/.*ok=/,"",$0); split($0,a," "); ok2 += a[1]; fail += $3 + 0 }
' "$LOADGEN_LOG" >/dev/null 2>&1 || true
total_ok=$(awk -F'[= ]' '/^worker=/ {ok += $4} END {print ok+0}' "$LOADGEN_LOG")
total_fail=$(awk -F'[= ]' '/^worker=/ {fail += $6} END {print fail+0}' "$LOADGEN_LOG")
log "  HTTP requests through SOCKS5: ok=$total_ok fail=$total_fail"

# Churn summary.
cycles=$(awk -F= '/^churn: total cycles=/ {print $2}' "$CHURN_LOG")
cycles="${cycles:-0}"
log "  Churn cycles executed: $cycles"

# Analyze.
log "analyzing sampler log..."
set +e
./analyze.sh "$SAMPLER_LOG" "$cycles" | tee -a "$RUN_LOG"
analysis_rc=${PIPESTATUS[0]}
set -e

if $DO_TEARDOWN; then
  log "tearing down..."
  docker compose -p muti-metrooleak down >>"$RUN_LOG" 2>&1
else
  log "leaving containers running (--no-teardown)"
fi

log "done. analysis exit=$analysis_rc"
exit "$analysis_rc"
