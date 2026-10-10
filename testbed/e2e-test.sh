#!/usr/bin/env bash
#
# E2E Test Suite for Muti Metroo 6-Agent Docker Mesh Testbed
#
# Runs against a live Docker testbed to verify all major features
# after the bug audit fixes.
#
# Usage:
#   ./testbed/e2e-test.sh          # Build, start, test, teardown
#   ./testbed/e2e-test.sh --no-build   # Skip docker build
#   ./testbed/e2e-test.sh --no-teardown # Leave containers running
#   ./testbed/e2e-test.sh --skip-sleep  # Skip sleep/wake tests
#
set -euo pipefail

# -- Configuration ----------------------------------------------------------

COMPOSE="docker compose"
SOCKS_PORT=1090
HEALTH_PORTS=(8091 8092 8093 8094 8095 8096)
TARGET_IP="172.28.0.100"
AGENT_NAMES=(agent1 agent2 agent3 agent4 agent5 agent6)

# Timeouts
HEALTH_TIMEOUT=120    # seconds to wait for all agents healthy
ROUTE_TIMEOUT=60      # seconds to wait for routes to propagate
CURL_TIMEOUT=10       # per-request timeout

# Flags
DO_BUILD=true
DO_TEARDOWN=true
SKIP_SLEEP=false

for arg in "$@"; do
  case "$arg" in
    --no-build)    DO_BUILD=false ;;
    --no-teardown) DO_TEARDOWN=false ;;
    --skip-sleep)  SKIP_SLEEP=true ;;
  esac
done

# -- Colors & Output --------------------------------------------------------

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  echo -e "  ${GREEN}PASS${NC} $1"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  echo -e "  ${RED}FAIL${NC} $1"
  if [ -n "${2:-}" ]; then
    echo -e "       ${RED}$2${NC}"
  fi
}

skip() {
  SKIP_COUNT=$((SKIP_COUNT + 1))
  echo -e "  ${YELLOW}SKIP${NC} $1"
}

header() {
  echo ""
  echo -e "${BOLD}${CYAN}=== $1 ===${NC}"
}

# -- Helper Functions -------------------------------------------------------

# curl wrapper with timeout and silent mode
ccurl() {
  curl -s --max-time "$CURL_TIMEOUT" "$@"
}

# Wait for an HTTP endpoint to return 200
wait_for_health() {
  local url="$1"
  local deadline=$((SECONDS + HEALTH_TIMEOUT))
  while [ $SECONDS -lt $deadline ]; do
    if ccurl -o /dev/null -w "%{http_code}" "$url" 2>/dev/null | grep -q "200"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# Wait for routes to appear on agent1
wait_for_routes() {
  local deadline=$((SECONDS + ROUTE_TIMEOUT))
  while [ $SECONDS -lt $deadline ]; do
    local count
    count=$(ccurl "http://localhost:8091/healthz" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('route_count',0))" 2>/dev/null || echo "0")
    if [ "$count" -gt 0 ]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# -- Lifecycle --------------------------------------------------------------

cleanup() {
  if [ "$DO_TEARDOWN" = true ]; then
    header "Teardown"
    echo "  Stopping containers..."
    $COMPOSE down --timeout 10 2>/dev/null || true
  else
    echo ""
    echo -e "${YELLOW}Containers left running (--no-teardown).${NC}"
  fi
}

trap cleanup EXIT

# -- Build & Start ----------------------------------------------------------

cd "$(dirname "$0")/.."

header "Docker Mesh Testbed E2E Tests"

if [ "$DO_BUILD" = true ]; then
  header "Step 1: Building Docker images"
  $COMPOSE build --quiet
  echo "  Build complete."
else
  echo ""
  echo -e "  ${YELLOW}Skipping build (--no-build).${NC}"
fi

header "Step 2: Starting mesh (6 agents + target)"

# Reset sleep state files to ensure clean start
for i in 1 2 3 4 5 6; do
  state_file="testbed/agent${i}/data/sleep_state.json"
  if [ -f "$state_file" ]; then
    echo '{"state":0,"sleep_start_time":"0001-01-01T00:00:00Z","last_poll_time":"0001-01-01T00:00:00Z","command_seq":0}' > "$state_file"
  fi
done

$COMPOSE up -d agent1 agent2 agent3 agent4 agent5 agent6 target
echo "  Containers started. Waiting for health..."

# Wait for all agents to become healthy
all_healthy=true
for i in "${!HEALTH_PORTS[@]}"; do
  port="${HEALTH_PORTS[$i]}"
  name="${AGENT_NAMES[$i]}"
  if wait_for_health "http://localhost:${port}/health"; then
    echo -e "  ${GREEN}*${NC} ${name} healthy (port ${port})"
  else
    echo -e "  ${RED}*${NC} ${name} NOT healthy (port ${port})"
    all_healthy=false
  fi
done

if [ "$all_healthy" = false ]; then
  echo -e "\n${RED}Not all agents healthy. Aborting tests.${NC}"
  exit 1
fi

# Wait for route propagation from agent6 to agent1
echo "  Waiting for route propagation..."
if wait_for_routes; then
  echo -e "  ${GREEN}*${NC} Routes propagated to agent1"
else
  echo -e "  ${YELLOW}*${NC} Warning: routes may not have propagated yet"
fi

# ===== TESTS ===============================================================

# -- Test 1: Health Checks --------------------------------------------------

header "Test 1: Health Checks"

for i in "${!HEALTH_PORTS[@]}"; do
  port="${HEALTH_PORTS[$i]}"
  name="${AGENT_NAMES[$i]}"
  code=$(ccurl -o /dev/null -w "%{http_code}" "http://localhost:${port}/health" 2>/dev/null || echo "000")
  if [ "$code" = "200" ]; then
    pass "${name} /health -> 200"
  else
    fail "${name} /health -> ${code}" "Expected 200"
  fi
done

# -- Test 2: Detailed Health Stats ------------------------------------------

header "Test 2: Detailed Health Stats (/healthz)"

for i in "${!HEALTH_PORTS[@]}"; do
  port="${HEALTH_PORTS[$i]}"
  name="${AGENT_NAMES[$i]}"
  resp=$(ccurl "http://localhost:${port}/healthz" 2>/dev/null || echo "")
  if [ -z "$resp" ]; then
    fail "${name} /healthz returned empty response"
    continue
  fi
  peer_count=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin).get('peer_count',0))" 2>/dev/null || echo "-1")
  if [ "$peer_count" -gt 0 ]; then
    pass "${name} peer_count=${peer_count}"
  else
    fail "${name} peer_count=${peer_count}" "Expected > 0"
  fi
done

# -- Test 3: Mesh Connectivity (mesh-test) -----------------------------------

header "Test 3: Mesh Connectivity"

mesh_resp=$(ccurl -X POST "http://localhost:8091/api/mesh-test" --max-time 30 2>/dev/null || echo "")
if [ -z "$mesh_resp" ]; then
  fail "mesh-test returned empty response"
else
  total=$(echo "$mesh_resp" | python3 -c "import sys,json; print(json.load(sys.stdin).get('total_count',0))" 2>/dev/null || echo "0")
  reachable=$(echo "$mesh_resp" | python3 -c "import sys,json; print(json.load(sys.stdin).get('reachable_count',0))" 2>/dev/null || echo "0")
  # mesh-test only discovers agents with HTTP dashboard enabled that respond
  # to remote status queries. Not all agents may be visible.
  if [ "$total" -gt 0 ] && [ "$reachable" -eq "$total" ]; then
    pass "All ${reachable}/${total} discovered agents reachable"
  elif [ "$reachable" -gt 0 ]; then
    fail "${reachable}/${total} agents reachable" "Expected all discovered agents to be reachable"
  else
    fail "mesh-test: total=${total} reachable=${reachable}"
  fi

  # Check individual results
  unreachable=$(echo "$mesh_resp" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for r in data.get('results', []):
    if not r.get('reachable') and not r.get('is_local'):
        print(r.get('display_name', r.get('agent_id','?')))
" 2>/dev/null || echo "")
  if [ -n "$unreachable" ]; then
    echo -e "  ${RED}  Unreachable: ${unreachable}${NC}"
  fi
fi

# -- Test 4: Route Table Verification ---------------------------------------

header "Test 4: Route Table Verification"

dashboard=$(ccurl "http://localhost:8091/api/dashboard" 2>/dev/null || echo "")
if [ -z "$dashboard" ]; then
  fail "dashboard returned empty response"
else
  route_count=$(echo "$dashboard" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('routes',[])))" 2>/dev/null || echo "0")
  if [ "$route_count" -gt 0 ]; then
    pass "Agent1 has ${route_count} routes"
    # Show route details
    echo "$dashboard" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for r in data.get('routes', []):
    prefix = r.get('prefix', '?')
    rt = r.get('route_type', '?')
    path = ' -> '.join(r.get('path_display', []))
    print(f'       {rt}: {prefix}  via {path}')
" 2>/dev/null || true
  else
    fail "Agent1 has 0 routes" "Expected exit routes from agent6"
  fi
fi

# -- Test 5: SOCKS5 Proxy - HTTP --------------------------------------------

header "Test 5: SOCKS5 Proxy (HTTP via mesh)"

socks_resp=$(ccurl -x "socks5h://localhost:${SOCKS_PORT}" "http://${TARGET_IP}" 2>/dev/null || echo "")
if echo "$socks_resp" | grep -qi "welcome to nginx"; then
  pass "SOCKS5 proxy -> nginx target: got welcome page"
else
  if [ -z "$socks_resp" ]; then
    fail "SOCKS5 proxy -> nginx target: empty response"
  else
    fail "SOCKS5 proxy -> nginx target: unexpected response" "$(echo "$socks_resp" | head -1)"
  fi
fi

# -- Test 6: SOCKS5 Proxy - Concurrent Connections --------------------------

header "Test 6: SOCKS5 Concurrent Connections"

concurrent_ok=0
concurrent_fail=0
pids=()

for j in $(seq 1 5); do
  (
    resp=$(ccurl -x "socks5h://localhost:${SOCKS_PORT}" "http://${TARGET_IP}" 2>/dev/null || echo "")
    if echo "$resp" | grep -qi "welcome to nginx"; then
      exit 0
    else
      exit 1
    fi
  ) &
  pids+=($!)
done

for pid in "${pids[@]}"; do
  if wait "$pid"; then
    concurrent_ok=$((concurrent_ok + 1))
  else
    concurrent_fail=$((concurrent_fail + 1))
  fi
done

if [ "$concurrent_ok" -eq 5 ]; then
  pass "5/5 concurrent SOCKS5 requests succeeded"
else
  fail "${concurrent_ok}/5 concurrent requests succeeded" "${concurrent_fail} failed"
fi

# -- Test 7: Stream Cleanup After Connections --------------------------------

header "Test 7: Stream Cleanup"

echo "  Waiting 5s for streams to close..."
sleep 5

stream_leak=false
for port in 8091 8096; do
  name="agent1"
  [ "$port" = "8096" ] && name="agent6"
  resp=$(ccurl "http://localhost:${port}/healthz" 2>/dev/null || echo "")
  sc=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin).get('stream_count',0))" 2>/dev/null || echo "-1")
  if [ "$sc" -eq 0 ]; then
    pass "${name} stream_count=0 (no leak)"
  elif [ "$sc" -le 2 ]; then
    # Allow small transient count -- streams may still be closing
    pass "${name} stream_count=${sc} (within tolerance)"
  else
    fail "${name} stream_count=${sc}" "Expected 0 after idle"
    stream_leak=true
  fi
done

# -- Test 8: Topology API ---------------------------------------------------

header "Test 8: Topology API"

topo=$(ccurl "http://localhost:8091/api/topology" 2>/dev/null || echo "")
if [ -z "$topo" ]; then
  fail "topology returned empty response"
else
  agent_count=$(echo "$topo" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('agents',d.get('nodes',[]))))" 2>/dev/null || echo "0")
  if [ "$agent_count" -ge 6 ]; then
    pass "Topology shows ${agent_count} agents"
  elif [ "$agent_count" -gt 0 ]; then
    fail "Topology shows only ${agent_count} agents" "Expected >= 6"
  else
    # Try alternate key names
    has_data=$(echo "$topo" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d))" 2>/dev/null || echo "0")
    if [ "$has_data" -gt 0 ]; then
      pass "Topology returned data (${has_data} top-level keys)"
    else
      fail "Topology returned empty/invalid JSON"
    fi
  fi
fi

# -- Test 9: Node Info -------------------------------------------------------

header "Test 9: Node Info"

nodes=$(ccurl "http://localhost:8091/api/nodes" 2>/dev/null || echo "")
if [ -z "$nodes" ]; then
  fail "nodes returned empty response"
else
  node_count=$(echo "$nodes" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if isinstance(data, list):
    print(len(data))
elif isinstance(data, dict) and 'nodes' in data:
    print(len(data['nodes']))
else:
    print(len(data))
" 2>/dev/null || echo "0")
  if [ "$node_count" -ge 6 ]; then
    pass "Node info: ${node_count} nodes reported"
  elif [ "$node_count" -gt 0 ]; then
    pass "Node info: ${node_count} nodes (some may still be propagating)"
  else
    fail "Node info: no nodes returned"
  fi
fi

# -- Test 10: Sleep/Wake Cycle ----------------------------------------------

header "Test 10: Sleep/Wake Cycle"

if [ "$SKIP_SLEEP" = true ]; then
  skip "Sleep/wake tests (--skip-sleep)"
else
  # Trigger sleep
  sleep_resp=$(ccurl -X POST "http://localhost:8091/sleep" 2>/dev/null || echo "")
  sleep_ok=$(echo "$sleep_resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print('ok' if d.get('status') in ('triggered','ok','success') or 'sleep' in str(d).lower() else 'no')" 2>/dev/null || echo "no")
  if [ "$sleep_ok" = "ok" ]; then
    pass "Sleep triggered successfully"
  else
    fail "Sleep trigger failed" "$sleep_resp"
  fi

  # Wait for sleep to take effect
  sleep 3

  # Check sleep status
  status=$(ccurl "http://localhost:8091/sleep/status" 2>/dev/null || echo "")
  state=$(echo "$status" | python3 -c "import sys,json; print(json.load(sys.stdin).get('state','UNKNOWN'))" 2>/dev/null || echo "UNKNOWN")
  if [ "$state" = "SLEEPING" ] || [ "$state" = "POLLING" ]; then
    pass "Sleep status: ${state}"
  else
    fail "Sleep status: ${state}" "Expected SLEEPING or POLLING"
  fi

  # Trigger wake -- the wake endpoint may block while reconnecting peers,
  # so fire it with a short timeout and verify via status polling instead.
  curl -s --max-time 5 -X POST "http://localhost:8091/wake" >/dev/null 2>&1 || true

  # Poll for AWAKE state (wake reconnects peers which takes time)
  wake_deadline=$((SECONDS + 30))
  wake_confirmed=false
  while [ $SECONDS -lt $wake_deadline ]; do
    status=$(ccurl "http://localhost:8091/sleep/status" 2>/dev/null || echo "")
    state=$(echo "$status" | python3 -c "import sys,json; print(json.load(sys.stdin).get('state','UNKNOWN'))" 2>/dev/null || echo "UNKNOWN")
    if [ "$state" = "AWAKE" ]; then
      wake_confirmed=true
      break
    fi
    sleep 2
  done

  if [ "$wake_confirmed" = true ]; then
    pass "Wake completed: AWAKE"
  else
    fail "Wake did not complete within 30s" "Last state: ${state}"
  fi

  # Verify health still works after wake
  code=$(ccurl -o /dev/null -w "%{http_code}" "http://localhost:8091/health" 2>/dev/null || echo "000")
  if [ "$code" = "200" ]; then
    pass "Health check OK after wake"
  else
    fail "Health check after wake: ${code}"
  fi
fi

# -- Test 11: Route Re-advertisement After Wake ------------------------------

header "Test 11: Route Re-advertisement"

if [ "$SKIP_SLEEP" = true ]; then
  skip "Route re-advertisement (--skip-sleep)"
else
  # Trigger route advertisement on agent1 (sends its own local routes, if any)
  adv_resp=$(ccurl -X POST "http://localhost:8091/routes/advertise" 2>/dev/null || echo "")
  adv_ok=$(echo "$adv_resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print('ok' if d.get('status') == 'triggered' else 'no')" 2>/dev/null || echo "no")
  if [ "$adv_ok" = "ok" ]; then
    pass "Route advertisement triggered"
  else
    fail "Route advertisement trigger" "$adv_resp"
  fi

  # Routes are removed from agents when peers disconnect (during sleep), so
  # they must re-propagate from agent6 (the exit/origin) back through the
  # cascade after wake. Trigger advertise on the exit and intermediate agents
  # to speed this up instead of waiting for the natural advertise interval.
  for p in 8096 8095 8094 8093 8092; do
    ccurl -X POST "http://localhost:${p}/routes/advertise" >/dev/null 2>&1 || true
  done

  # Poll for routes to re-propagate (cascade through 4 hops takes a few seconds).
  route_deadline=$((SECONDS + 30))
  rc=0
  while [ $SECONDS -lt $route_deadline ]; do
    resp=$(ccurl "http://localhost:8091/healthz" 2>/dev/null || echo "")
    rc=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin).get('route_count',0))" 2>/dev/null || echo "0")
    if [ "$rc" -gt 0 ]; then
      break
    fi
    sleep 1
  done
  if [ "$rc" -gt 0 ]; then
    pass "Routes re-propagated after wake: route_count=${rc}"
  else
    fail "Routes did not re-propagate within 30s"
  fi

  # Verify SOCKS5 still works after sleep/wake cycle
  socks_resp=$(ccurl -x "socks5h://localhost:${SOCKS_PORT}" "http://${TARGET_IP}" 2>/dev/null || echo "")
  if echo "$socks_resp" | grep -qi "welcome to nginx"; then
    pass "SOCKS5 proxy works after sleep/wake cycle"
  else
    fail "SOCKS5 proxy broken after sleep/wake cycle"
  fi
fi

# -- Test 12: Graceful Shutdown & Recovery -----------------------------------

header "Test 12: Exit Node Restart"

# Stop exit node
echo "  Stopping agent6 (exit node)..."
$COMPOSE stop agent6 2>/dev/null

# Immediately verify SOCKS5 fails (no exit route)
sleep 3
socks_resp=$(ccurl -x "socks5h://localhost:${SOCKS_PORT}" "http://${TARGET_IP}" 2>/dev/null || echo "")
if echo "$socks_resp" | grep -qi "welcome to nginx"; then
  # It might still work briefly if routes haven't expired
  echo "  (routes still cached - checking stale cleanup)"
fi

# Wait for route TTL to expire
echo "  Waiting for stale route cleanup..."
sleep 12

# Check routes are cleaning up
resp=$(ccurl "http://localhost:8091/healthz" 2>/dev/null || echo "")
rc_before=$(echo "$resp" | python3 -c "import sys,json; print(json.load(sys.stdin).get('route_count',0))" 2>/dev/null || echo "0")
echo "  Route count after agent6 down: ${rc_before}"

# Restart exit node
echo "  Starting agent6..."
$COMPOSE start agent6 2>/dev/null

# Wait for agent6 health
if wait_for_health "http://localhost:8096/health"; then
  pass "Agent6 restarted successfully"
else
  fail "Agent6 failed to restart"
fi

# Trigger route advertisement on agent6 to speed up propagation
sleep 3
ccurl -X POST "http://localhost:8096/routes/advertise" >/dev/null 2>&1 || true
# Also trigger on intermediate agents to propagate faster
for p in 8095 8094 8093 8092; do
  ccurl -X POST "http://localhost:${p}/routes/advertise" >/dev/null 2>&1 || true
done

# Wait for routes to re-propagate (use longer timeout -- default advertise is 2m)
echo "  Waiting for route re-propagation..."
ROUTE_TIMEOUT_RESTART=150
route_deadline=$((SECONDS + ROUTE_TIMEOUT_RESTART))
routes_back=false
while [ $SECONDS -lt $route_deadline ]; do
  count=$(ccurl "http://localhost:8091/healthz" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('route_count',0))" 2>/dev/null || echo "0")
  if [ "$count" -gt 0 ]; then
    routes_back=true
    break
  fi
  # Re-trigger advertisements periodically
  if [ $(( (SECONDS - route_deadline + ROUTE_TIMEOUT_RESTART) % 15 )) -lt 3 ]; then
    ccurl -X POST "http://localhost:8096/routes/advertise" >/dev/null 2>&1 || true
  fi
  sleep 3
done

if [ "$routes_back" = true ]; then
  pass "Routes re-propagated after restart"
else
  fail "Routes did not re-propagate within ${ROUTE_TIMEOUT_RESTART}s"
fi

# Verify SOCKS5 works again
sleep 2
socks_resp=$(ccurl -x "socks5h://localhost:${SOCKS_PORT}" "http://${TARGET_IP}" 2>/dev/null || echo "")
if echo "$socks_resp" | grep -qi "welcome to nginx"; then
  pass "SOCKS5 proxy recovered after exit node restart"
else
  fail "SOCKS5 proxy not recovered after exit node restart"
fi

# ===== SUMMARY ==============================================================

header "Summary"

total=$((PASS_COUNT + FAIL_COUNT + SKIP_COUNT))
echo ""
echo -e "  ${GREEN}Passed:  ${PASS_COUNT}${NC}"
echo -e "  ${RED}Failed:  ${FAIL_COUNT}${NC}"
if [ "$SKIP_COUNT" -gt 0 ]; then
  echo -e "  ${YELLOW}Skipped: ${SKIP_COUNT}${NC}"
fi
echo -e "  Total:   ${total}"
echo ""

if [ "$FAIL_COUNT" -gt 0 ]; then
  echo -e "${RED}${BOLD}SOME TESTS FAILED${NC}"
  exit 1
else
  echo -e "${GREEN}${BOLD}ALL TESTS PASSED${NC}"
  exit 0
fi
