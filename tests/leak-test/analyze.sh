#!/usr/bin/env bash
# Reads the sampler CSV log and prints a per-container time series plus a
# pass/fail verdict. The leak (pre-61c6008) accumulates ESTABLISHED sockets
# on the DIALER side (transit) -- one extra socket per peer reconnect cycle.
#
# Verdict: for each container, fit a simple growth measure: max - min of the
# mesh_sockets series after a 60s warmup. Anything above LEAK_THRESHOLD per
# expected-peer suggests the leak is still present.
#
# Usage: ./analyze.sh <sampler_log> <expected_churn_cycles>
set -uo pipefail

LOG="${1:-/tmp/sampler.log}"
CYCLES="${2:-0}"

# After 24+ churn cycles, a leak would push mesh_sockets on transit far
# above the steady-state count (2 peers). Allow a small headroom for
# in-flight closes during the last sample.
LEAK_HEADROOM=4

if [ ! -s "$LOG" ]; then
  echo "no sampler data at $LOG"
  exit 2
fi

printf '\n=== per-container time series (ts, peer_count, mesh_sockets) ===\n'
for c in muti-metroo-leak-ingress muti-metroo-leak-transit muti-metroo-leak-exit1 muti-metroo-leak-exit2; do
  printf '\n--- %s ---\n' "$c"
  awk -F, -v c="$c" '$2 == c {printf "  %s  peers=%-3s sockets=%s\n", $1, $3, $4}' "$LOG"
done

printf '\n=== summary (min / median / max mesh_sockets per container) ===\n'
awk -F, '
NR == 1 { next }
$4 != "NA" {
  c = $2
  v = $4 + 0
  rows[c, ++cnt[c]] = v
  if (!(c in minv) || v < minv[c]) minv[c] = v
  if (!(c in maxv) || v > maxv[c]) maxv[c] = v
}
END {
  for (c in cnt) {
    n = cnt[c]
    # cheap median via sort
    for (i = 1; i <= n; i++) a[i] = rows[c, i]
    for (i = 1; i <= n; i++) for (j = i+1; j <= n; j++) if (a[i] > a[j]) { t=a[i]; a[i]=a[j]; a[j]=t }
    med = (n % 2) ? a[(n+1)/2] : (a[n/2] + a[n/2+1]) / 2
    printf "  %-26s n=%-3d min=%-3d med=%-5g max=%-3d delta=%d\n", c, n, minv[c], med, maxv[c], maxv[c]-minv[c]
  }
}' "$LOG"

# Verdict: focus on transit (the dialer that historically leaked) and exits
# (listener side -- should also stay flat). Ingress is dialer toward
# transit so it's also in scope.
printf '\n=== verdict ===\n'
verdict=0
for c in muti-metroo-leak-ingress muti-metroo-leak-transit muti-metroo-leak-exit1 muti-metroo-leak-exit2; do
  read -r minv maxv <<<"$(awk -F, -v c="$c" '
    NR == 1 || $2 != c || $4 == "NA" { next }
    !seen { minv = $4; maxv = $4; seen = 1; next }
    { v = $4 + 0; if (v < minv) minv = v; if (v > maxv) maxv = v }
    END { if (seen) printf "%d %d\n", minv, maxv; else print "NA NA" }
  ' "$LOG")"
  if [ "$minv" = "NA" ]; then
    printf '  %-26s NO DATA\n' "$c"
    continue
  fi
  delta=$(( maxv - minv ))
  budget=$LEAK_HEADROOM
  status='PASS'
  if [ "$delta" -gt "$budget" ]; then
    status='FAIL'
    verdict=1
  fi
  printf '  %-26s min=%-3d max=%-3d delta=%-3d budget=%-3d  %s\n' \
    "$c" "$minv" "$maxv" "$delta" "$budget" "$status"
done

printf '\n  churn cycles driven: %d\n' "$CYCLES"
if [ "$verdict" -eq 0 ]; then
  printf '\n  OVERALL: PASS -- no socket accumulation across %d churn cycles\n' "$CYCLES"
else
  printf '\n  OVERALL: FAIL -- some container shows socket growth > headroom\n'
fi
exit "$verdict"
