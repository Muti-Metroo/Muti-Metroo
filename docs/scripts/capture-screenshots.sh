#!/usr/bin/env bash
# Regenerate every documentation screenshot from a throwaway fixture mesh.
#
# What it does, end to end:
#   1. Builds a Linux muti-metroo binary from this repository's committed
#      HEAD (uncommitted changes are ignored) and downloads the latest
#      Muti Metroo Manager release binary for this host.
#   2. Starts a four-agent fixture mesh in Docker containers with fixed
#      agent IDs, display names, hostnames, and IP addresses:
#
#        gateway --QUIC-- transit --HTTP/2-- exit-datacenter
#           \
#            --WebSocket-- exit-cloud
#
#      gateway runs SOCKS5 and the HTTP API that Manager proxies, and holds
#      the fixture's fixed management and signing private keys. gateway and
#      transit advertise their listeners as dial targets. exit-datacenter
#      allows management commands and file transfer. Both exits allow ICMP.
#      Sleep mode is enabled everywhere, so Manager offers Suspend mesh.
#   3. Stages a Manager downloads bundle from the latest GitHub releases
#      (cached in docs/scripts/.cache, keyed by release tag).
#   4. Starts Manager on the host against gateway and runs
#      capture-muti-metroo-manager.js.
#   5. Removes the containers, the Docker network, and the temp directory,
#      also when a step fails.
#
# Containers keep the host's name and addresses out of the screenshots, and
# the fixed identities mean every run photographs the same mesh. Only live
# values (uptime, RTT, byte counters) differ between runs.
#
# Requirements: Go, Docker, Node.js 20+, gh (authenticated), zip.
#
# Usage:
#   docs/scripts/capture-screenshots.sh
#
# Environment:
#   MANAGER_BIN    use this Manager binary instead of the latest release
#   PORT_BASE      first port of the host port block (default 27100)
#   KEEP_RUNNING=1 leave the mesh and Manager up after capturing
#   HEADED=1       show the browser while capturing

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PORT_BASE="${PORT_BASE:-27100}"
CACHE_DIR="$SCRIPT_DIR/.cache"
IMAGE="mm-docs-shots-agent:1"
NETWORK="mm-docs-shots"
SUBNET="10.99.0.0/24"

# Fixed identities. The capture script picks agents by sorting full IDs, so
# this order is part of the fixture: gateway sorts first.
GATEWAY_ID="1a2b3c4d5e6f40718293a4b5c6d7e8f9"
TRANSIT_ID="2b3c4d5e6f70418293a4b5c6d7e8f901"
EXIT_DC_ID="3c4d5e6f708142939a4b5c6d7e8f9012"
EXIT_CLOUD_ID="4d5e6f70819243a4b5c6d7e8f9012345"

AGENTS=(gateway transit exit-datacenter exit-cloud)
# One-way egress delay per container, so links show realistic RTTs: the two
# datacenter links land in the green band and the cloud link in the amber one.
agent_delay() {
  case "$1" in
    gateway) echo 3ms ;;
    transit) echo 6ms ;;
    exit-datacenter) echo 8ms ;;
    exit-cloud) echo 70ms ;;
  esac
}

agent_ip() {
  case "$1" in
    gateway) echo 10.99.0.10 ;;
    transit) echo 10.99.0.20 ;;
    exit-datacenter) echo 10.99.0.30 ;;
    exit-cloud) echo 10.99.0.40 ;;
  esac
}

HTTP_GATEWAY=$((PORT_BASE + 20))
MANAGER_PORT=$((PORT_BASE + 30))

WORK="$(mktemp -d "${TMPDIR:-/tmp}/mm-docs-shots.XXXXXX")"
MANAGER_PID=""

log() { printf '==> %s\n' "$*"; }

teardown() {
  [ -n "$MANAGER_PID" ] && kill "$MANAGER_PID" 2>/dev/null || true
  for name in "${AGENTS[@]}"; do
    docker rm -f "mm-docs-$name" >/dev/null 2>&1 || true
  done
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}

cleanup() {
  local rc=$?
  if [ "${KEEP_RUNNING:-}" = "1" ]; then
    log "KEEP_RUNNING=1: Manager at http://127.0.0.1:$MANAGER_PORT (pid $MANAGER_PID)"
    log "Containers: ${AGENTS[*]/#/mm-docs-}; work dir $WORK"
    return
  fi
  if [ $rc -ne 0 ]; then
    for name in "${AGENTS[@]}"; do
      echo "--- $name ---" >&2
      docker logs --tail 15 "mm-docs-$name" >&2 2>&1 || true
    done
    [ -f "$WORK/manager.log" ] && { echo "--- manager ---" >&2; tail -n 15 "$WORK/manager.log" >&2; }
  fi
  teardown
  exit $rc
}
trap cleanup EXIT INT TERM

for port in $HTTP_GATEWAY $MANAGER_PORT; do
  if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "Port $port is in use. Set PORT_BASE to a free block." >&2
    exit 1
  fi
done

# Leftovers from an interrupted run would hold the names and the subnet.
for name in "${AGENTS[@]}"; do
  docker rm -f "mm-docs-$name" >/dev/null 2>&1 || true
done
docker network rm "$NETWORK" >/dev/null 2>&1 || true

# --- Build -------------------------------------------------------------------

DOCKER_ARCH="$(docker version --format '{{.Server.Arch}}')"
# Build from committed HEAD, not the working tree, so uncommitted work in
# progress never shows up in the screenshots. Stamp the latest release
# version so the UI shows what readers run, not a dev build string.
log "Building muti-metroo from HEAD (linux/$DOCKER_ARCH and host)"
mkdir -p "$WORK/bin" "$WORK/src"
git -C "$REPO_ROOT" archive HEAD | tar -x -C "$WORK/src"
VERSION="$(git -C "$REPO_ROOT" describe --tags --abbrev=0 --match 'muti-metroo-v*' | sed 's/^muti-metroo-v//')"
LDFLAGS="-s -w -X main.Version=$VERSION"
(cd "$WORK/src" && CGO_ENABLED=0 GOOS=linux GOARCH="$DOCKER_ARCH" \
  go build -trimpath -ldflags "$LDFLAGS" -o "$WORK/bin/muti-metroo-linux" ./cmd/muti-metroo)
MM="$WORK/bin/muti-metroo"
(cd "$WORK/src" && go build -trimpath -ldflags "$LDFLAGS" -o "$MM" ./cmd/muti-metroo)

# Muti Metroo Manager: the latest release binary for this host, cached by
# tag. MANAGER_BIN points at another build instead (for example to capture
# an unreleased Manager on purpose).
if [ -n "${MANAGER_BIN:-}" ]; then
  MANAGER="$MANAGER_BIN"
else
  case "$(uname -s)" in Darwin) host_os=darwin ;; Linux) host_os=linux ;; *) echo "Unsupported host OS" >&2; exit 1 ;; esac
  case "$(uname -m)" in arm64|aarch64) host_arch=arm64 ;; x86_64|amd64) host_arch=amd64 ;; *) echo "Unsupported host arch" >&2; exit 1 ;; esac
  mgr_tag="$(gh release view --repo Muti-Metroo/Muti-Metroo-Manager --json tagName --jq .tagName)"
  MANAGER="$CACHE_DIR/$mgr_tag/metroo-manager-$host_os-$host_arch"
  if [ ! -x "$MANAGER" ]; then
    log "Downloading Muti Metroo Manager $mgr_tag"
    mkdir -p "$CACHE_DIR/$mgr_tag"
    gh release download "$mgr_tag" --repo Muti-Metroo/Muti-Metroo-Manager --dir "$CACHE_DIR/$mgr_tag" \
      --pattern "metroo-manager-$host_os-$host_arch" --clobber
    chmod +x "$MANAGER"
    xattr -d com.apple.quarantine "$MANAGER" 2>/dev/null || true
  fi
fi
log "Using Muti Metroo Manager: $("$MANAGER" version 2>/dev/null | head -1)"

if [ ! -d "$SCRIPT_DIR/node_modules/playwright" ]; then
  log "Installing Playwright"
  (cd "$SCRIPT_DIR" && npm install --silent && npx playwright install chromium)
fi

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  log "Building fixture image $IMAGE"
  docker build --platform "linux/$DOCKER_ARCH" -t "$IMAGE" - >/dev/null <<'DOCKERFILE'
FROM debian:12-slim
RUN apt-get update && apt-get install -y --no-install-recommends iproute2 \
    && rm -rf /var/lib/apt/lists/*
DOCKERFILE
fi

# --- Keys ----------------------------------------------------------------------

# Fresh key pairs on every run; no key material lives in the repository. The
# only place a key shows up in a screenshot is the agent config wizard's
# YAML, and the capture script replaces the public keys there with fixed
# placeholder values before taking that shot.
log "Generating management and signing keys"
"$MM" management-key generate --out "$WORK/keys" >/dev/null
"$MM" signing-key generate --out "$WORK/keys" >/dev/null
read_key() { tr -d '[:space:]' < "$WORK/keys/$1"; }
MGMT_PUB="$(read_key management-public.key)"
MGMT_PRIV="$(read_key management-private.key)"
SIGN_PUB="$(read_key signing-public.key)"
SIGN_PRIV="$(read_key signing-private.key)"

# --- Fixture mesh ----------------------------------------------------------------

mkdir -p "$WORK/files/reports" "$WORK/files/configs"
printf 'Q3 summary\n' > "$WORK/files/reports/q3-summary.txt"
printf 'Q4 forecast\n' > "$WORK/files/reports/q4-forecast.txt"
printf 'listeners: []\n' > "$WORK/files/configs/edge.yaml"
# Fixed timestamps, so listings in the shell and file browser shots match
# between runs.
find "$WORK/files" -exec touch -t 202601151030 {} +

# write_config NAME ID EXTRA_YAML
write_config() {
  local name="$1" id="$2" extra="$3"
  mkdir -p "$WORK/$name"
  cat > "$WORK/$name/config.yaml" <<EOF
agent:
  id: "$id"
  display_name: "$name"
  data_dir: "/var/lib/muti-metroo"
  log_level: "warn"
http:
  enabled: true
  address: "0.0.0.0:8080"
connections:
  idle_threshold: 5s
routing:
  node_info_interval: 10s
sleep:
  enabled: true
management:
  public_key: "$MGMT_PUB"
  signing_public_key: "$SIGN_PUB"
$extra
EOF
  "$MM" config validate "$WORK/$name/config.yaml" >/dev/null
}

write_config gateway "$GATEWAY_ID" "  private_key: \"$MGMT_PRIV\"
  signing_private_key: \"$SIGN_PRIV\"
listeners:
  - transport: quic
    address: \"0.0.0.0:4433\"
    advertise:
      address: \"gateway.example.net:4433\"
  - transport: ws
    address: \"0.0.0.0:8443\"
    path: \"/mesh\"
    advertise:
      address: \"gateway.example.net:443\"
socks5:
  enabled: true
  address: \"0.0.0.0:1080\""

write_config transit "$TRANSIT_ID" "listeners:
  - transport: h2
    address: \"0.0.0.0:4443\"
    path: \"/mesh\"
    advertise:
      address: \"transit.example.net:4443\"
peers:
  - id: \"$GATEWAY_ID\"
    transport: quic
    address: \"gateway:4433\""

write_config exit-datacenter "$EXIT_DC_ID" "peers:
  - id: \"$TRANSIT_ID\"
    transport: h2
    address: \"transit:4443\"
    path: \"/mesh\"
exit:
  enabled: true
  routes:
    - cidr: \"10.20.0.0/16\"
      comment: \"datacenter LAN\"
    - cidr: \"10.99.0.0/24\"
      comment: \"lab network\"
  domain_routes:
    - \"*.corp.example.com\"
shell:
  enabled: true
  whitelist: [\"*\"]
file_transfer:
  enabled: true
  allowed_paths: [\"/srv/files/**\"]
icmp:
  enabled: true"

write_config exit-cloud "$EXIT_CLOUD_ID" "peers:
  - id: \"$GATEWAY_ID\"
    transport: ws
    address: \"wss://gateway:8443/mesh\"
exit:
  enabled: true
  routes:
    - cidr: \"172.31.0.0/16\"
      comment: \"cloud VPC\"
    - cidr: \"0.0.0.0/0\"
      metric: 10
      comment: \"internet egress\"
icmp:
  enabled: true"

log "Starting fixture mesh"
docker network create --subnet "$SUBNET" "$NETWORK" >/dev/null
for name in "${AGENTS[@]}"; do
  publish="--publish=127.0.0.1:$HTTP_GATEWAY:8080"
  [ "$name" = "gateway" ] || publish="--label=mm-docs-shots"
  docker run -d --platform "linux/$DOCKER_ARCH" --name "mm-docs-$name" --hostname "$name" \
    --network "$NETWORK" --ip "$(agent_ip "$name")" \
    "$publish" \
    -v "$WORK/bin/muti-metroo-linux:/usr/local/bin/muti-metroo:ro" \
    -v "$WORK/$name/config.yaml:/etc/muti-metroo/config.yaml:ro" \
    -v "$WORK/files:/srv/files" \
    --cap-add NET_ADMIN \
    "$IMAGE" sh -c "tc qdisc add dev eth0 root netem delay $(agent_delay "$name") && exec muti-metroo run -c /etc/muti-metroo/config.yaml" >/dev/null
done

wait_for() {
  local what="$1" tries="$2"
  shift 2
  for _ in $(seq 1 "$tries"); do
    if "$@" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "Timed out waiting for $what." >&2
  return 1
}

mesh_converged() {
  # All four agents visible, every exit route learned by gateway, and an
  # RTT measured on all three links.
  local topology agents rtts routes
  topology="$(curl -fsS "http://127.0.0.1:$HTTP_GATEWAY/api/topology")"
  agents="$(printf '%s' "$topology" | grep -o '"short_id"' | wc -l)"
  rtts="$(printf '%s' "$topology" | grep -o '"rtt_ms":[1-9]' | wc -l)"
  routes="$(curl -fsS "http://127.0.0.1:$HTTP_GATEWAY/healthz" | grep -o '"route_count":[0-9]*' | cut -d: -f2)"
  [ "${agents:-0}" -ge 4 ] && [ "${rtts:-0}" -ge 3 ] && [ "${routes:-0}" -ge 4 ]
}

wait_for "gateway HTTP API" 30 curl -fsS "http://127.0.0.1:$HTTP_GATEWAY/health"
wait_for "mesh convergence" 120 mesh_converged

# --- Downloads bundle ----------------------------------------------------------------

mm_tag="$(gh release view --repo Muti-Metroo/Muti-Metroo --json tagName --jq .tagName)"
ma_tag="$(gh release view --repo Muti-Metroo/Mutiauk --json tagName --jq .tagName)"
assets="$CACHE_DIR/$mm_tag+$ma_tag"
if [ ! -f "$assets/.complete" ]; then
  log "Downloading release assets for the downloads bundle ($mm_tag, $ma_tag)"
  rm -rf "$assets" && mkdir -p "$assets"
  gh release download "$mm_tag" --repo Muti-Metroo/Muti-Metroo --dir "$assets" --pattern 'muti-metroo-*'
  gh release download "$ma_tag" --repo Muti-Metroo/Mutiauk --dir "$assets" --pattern 'mutiauk-*'
  touch "$assets/.complete"
fi
node "$SCRIPT_DIR/make-downloads-bundle.mjs" "$assets" "${mm_tag##*-}" "${ma_tag##*-}" \
  "$WORK/muti-metroo-downloads-bundle.zip"

# --- Manager + capture -----------------------------------------------------------------

log "Starting Muti Metroo Manager"
"$MANAGER" -addr "127.0.0.1:$MANAGER_PORT" \
  -agent "http://127.0.0.1:$HTTP_GATEWAY" \
  -data-dir "$WORK/manager" \
  -downloads-bundle "$WORK/muti-metroo-downloads-bundle.zip" \
  > "$WORK/manager.log" 2>&1 &
MANAGER_PID=$!
wait_for "Manager" 30 curl -fsS "http://127.0.0.1:$MANAGER_PORT/"

log "Capturing screenshots"
(cd "$SCRIPT_DIR" && METROO_MANAGER_URL="http://127.0.0.1:$MANAGER_PORT" \
  HEADED="${HEADED:-}" node capture-muti-metroo-manager.js)

log "Done"
