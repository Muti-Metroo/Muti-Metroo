---
title: Docker Deployment
description: Build a Muti Metroo container image and run one or more agents with Docker and Docker Compose.
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole deploying to Docker" style={{maxWidth: '180px'}} />
</div>

# Docker Deployment

Muti Metroo runs in a container like any other single-binary service. No prebuilt image is published, so you build the image yourself, either from a release binary or from source.

## Build an image

### From a release binary

This Dockerfile downloads the Linux release binary for the build platform:

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache ca-certificates

ARG TARGETARCH
ADD --chmod=755 https://github.com/postalsys/Muti-Metroo/releases/latest/download/muti-metroo-linux-${TARGETARCH} /usr/local/bin/muti-metroo

WORKDIR /app
ENTRYPOINT ["muti-metroo"]
CMD ["run", "-c", "/app/config.yaml"]
```

```bash
docker build -t muti-metroo .
```

`TARGETARCH` is set by BuildKit (`amd64` or `arm64`). To pin a release, replace `latest/download` with `download/<tag>`.

### From source

The repository's `Dockerfile` builds the binary from the checked-out source in a Go build stage and produces the same layout (`/usr/local/bin/muti-metroo`, working directory `/app`, default command `run -c /app/config.yaml`):

```bash
git clone https://github.com/postalsys/Muti-Metroo.git
cd Muti-Metroo
docker build -t muti-metroo .
```

## Configure the agent for a container

Mount the config at `/app/config.yaml`, keep the data directory on a volume so the agent ID survives container re-creation, and bind listeners to `0.0.0.0` so published ports reach them:

```yaml
agent:
  display_name: "${AGENT_NAME:-docker-agent}"
  data_dir: "/app/data"
  log_level: "${LOG_LEVEL:-info}"
  log_format: "json"

tls:
  ca: "/app/certs/ca.crt"
  cert: "/app/certs/agent1.crt"
  key: "/app/certs/agent1.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "0.0.0.0:1080"

http:
  enabled: true
  address: "0.0.0.0:8080"
```

`${VAR:-default}` references are expanded from the container environment when the config is loaded. See [Environment Variables](/configuration/environment-variables).

Generate certificates on the host and mount them read-only:

```bash
muti-metroo cert ca --cn "Docker Mesh CA" -o ./certs
muti-metroo cert agent --cn agent1 -o ./certs
```

`cert agent` names its output after the common name (`agent1.crt`, `agent1.key`) and signs with `./certs/ca.crt` and `./certs/ca.key` by default.

## Run a single container

```bash
docker run -d --name muti-metroo \
  -e AGENT_NAME=gateway \
  -v "$(pwd)/config.yaml:/app/config.yaml:ro" \
  -v "$(pwd)/data:/app/data" \
  -v "$(pwd)/certs:/app/certs:ro" \
  -p 4433:4433/udp \
  -p 127.0.0.1:1080:1080 \
  -p 127.0.0.1:8080:8080 \
  muti-metroo
```

QUIC listeners need the port published as UDP (`4433:4433/udp`). HTTP/2 and WebSocket listeners use TCP. The example publishes the SOCKS and HTTP API ports on the host's loopback interface only.

Use `--env-file .env` instead of individual `-e` flags to keep values such as password hashes out of the shell history.

## Docker Compose

```yaml
services:
  agent:
    image: muti-metroo
    restart: unless-stopped
    environment:
      AGENT_NAME: gateway
      LOG_LEVEL: info
    ports:
      - "4433:4433/udp"
      - "127.0.0.1:1080:1080"
      - "127.0.0.1:8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./data:/app/data
      - ./certs:/app/certs:ro
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "-", "http://127.0.0.1:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

The health check uses the `/health` endpoint, which returns `OK` while the agent runs. It requires `http.enabled: true`. `wget` is part of the Alpine base image.

### Several agents on one Compose network

Agents on the same Compose network reach each other by service name. Give each agent its own config file and data directory, and point peers at the other service's name:

```yaml
services:
  agent1:
    image: muti-metroo
    volumes:
      - ./configs/agent1.yaml:/app/config.yaml:ro
      - ./data/agent1:/app/data
      - ./certs:/app/certs:ro
    ports:
      - "127.0.0.1:1080:1080"

  agent2:
    image: muti-metroo
    volumes:
      - ./configs/agent2.yaml:/app/config.yaml:ro
      - ./data/agent2:/app/data
      - ./certs:/app/certs:ro
```

```yaml
# configs/agent1.yaml (excerpt)
peers:
  - id: "auto"
    transport: quic
    address: "agent2:4433"
```

`id: "auto"` accepts whatever agent ID the peer presents. Replace it with agent2's ID once it is known, so the connection is rejected if a different agent answers.

A complete multi-agent example with SOCKS ingress, split exit routing, management commands, and file transfer is in [`examples/docker-tryout/`](https://github.com/postalsys/Muti-Metroo/tree/master/examples/docker-tryout) in the repository. It builds the image from source with `docker compose up -d`.

## Operations

```bash
# Follow logs
docker compose logs -f agent

# Run CLI commands against the agent inside the container
docker compose exec agent muti-metroo status
docker compose exec agent muti-metroo peer status

# Restart after a config change
docker compose restart agent
```

Containers run the agent as root, so the mounted data directory needs no special ownership.

For QUIC-heavy deployments, `network_mode: host` avoids the overhead of Docker's UDP port forwarding. With host networking, `ports:` is ignored and listeners bind directly on the host.

## Troubleshooting

**The container exits immediately.** Read the error with `docker compose logs agent`. Typical causes are a missing `/app/config.yaml` mount, a validation error, or a certificate path that does not exist inside the container. Validate the mounted config with the image itself:

```bash
docker run --rm -v "$(pwd)/config.yaml:/app/config.yaml:ro" muti-metroo config validate /app/config.yaml
```

**Peers cannot connect from outside.** Check that the listener binds to `0.0.0.0`, not `127.0.0.1`, and that QUIC ports are published with `/udp`.

**A new agent ID on every start.** The data directory is not on a volume. Mount `/app/data`, or set `agent.id` and `agent.private_key` in the config.

## See Also

- [System Service](/deployment/system-service) - Running the agent directly on the host
- [Configuration Overview](/configuration/overview) - Every config section
- [Environment Variables](/configuration/environment-variables) - `${VAR}` substitution in configs
- [CLI - cert](/cli/cert) - Generating CA and agent certificates
- [High Availability](/deployment/high-availability) - Redundant paths and exits
