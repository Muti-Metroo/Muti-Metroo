---
title: Docker Deployment
sidebar_position: 3
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-climbing.png" alt="Mole deploying to Docker" style={{maxWidth: '180px'}} />
</div>

# Docker Deployment

Run agents in containers for consistent, isolated deployments you can spin up anywhere Docker runs - your laptop, a server, or the cloud.

**What you get:**

- Same agent behavior on any system
- Easy multi-agent testing with Docker Compose
- Clean teardown - remove container, everything is gone
- No host system dependencies

## Docker Image

### Create Dockerfile

Create a Dockerfile using the pre-built binary:

```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates wget

# Download the binary (adjust URL for your platform)
ARG TARGETARCH
RUN wget -O /usr/local/bin/muti-metroo \
    https://github.com/postalsys/Muti-Metroo/releases/latest/download/muti-metroo-linux-${TARGETARCH} && \
    chmod +x /usr/local/bin/muti-metroo

WORKDIR /app
ENTRYPOINT ["muti-metroo"]
CMD ["run", "-c", "/app/config.yaml"]
```

### Build Image

```bash
# Build for current platform
docker build -t muti-metroo .

# Build with version tag
docker build -t muti-metroo:v1.0.0 .
```

## Running a Single Container

### Basic Run

```bash
docker run -d --name muti-metroo \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/certs:/app/certs \
  -p 1080:1080 \
  -p 4433:4433/udp \
  -p 8080:8080 \
  muti-metroo
```

### With Environment Variables

```bash
docker run -d --name muti-metroo \
  -e LOG_LEVEL=debug \
  -e SOCKS_ADDR=0.0.0.0:1080 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/certs:/app/certs \
  -p 1080:1080 \
  -p 4433:4433/udp \
  -p 8080:8080 \
  muti-metroo
```

### With Secrets

```bash
docker run -d --name muti-metroo \
  --env-file .env \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/certs:/app/certs \
  -p 1080:1080 \
  -p 4433:4433/udp \
  -p 8080:8080 \
  muti-metroo
```

## Docker Compose

### Basic Setup

```yaml
# docker-compose.yml
version: "3.8"

services:
  agent:
    image: muti-metroo
    restart: unless-stopped
    ports:
      - "1080:1080" # SOCKS5
      - "4433:4433/udp" # QUIC
      - "8080:8080" # HTTP API
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./data:/app/data
      - ./certs:/app/certs:ro
    environment:
      - LOG_LEVEL=info
```

:::note
Build your image first with `docker build -t muti-metroo .` before running docker compose.
:::

### Multi-Agent Testbed

For a ready-to-run 6-agent example with SOCKS5 proxy, split exit routing, management commands, and file transfer, see [`examples/docker-tryout/`](https://github.com/postalsys/Muti-Metroo/tree/master/examples/docker-tryout) in the repository.

```yaml
# docker-compose.yml
version: "3.8"

services:
  # Ingress agent
  agent1:
    image: muti-metroo
    restart: unless-stopped
    ports:
      - "1081:1080"
      - "8081:8080"
      - "4433:4433/udp"
    volumes:
      - ./configs/agent1.yaml:/app/config.yaml:ro
      - ./data/agent1:/app/data
      - ./certs:/app/certs:ro
    networks:
      - mesh

  # Transit agent
  agent2:
    image: muti-metroo
    restart: unless-stopped
    ports:
      - "1082:1080"
      - "8082:8080"
      - "4434:4433/udp"
    volumes:
      - ./configs/agent2.yaml:/app/config.yaml:ro
      - ./data/agent2:/app/data
      - ./certs:/app/certs:ro
    networks:
      - mesh

  # Exit agent
  agent3:
    image: muti-metroo
    restart: unless-stopped
    ports:
      - "1083:1080"
      - "8083:8080"
      - "4435:4433/udp"
    volumes:
      - ./configs/agent3.yaml:/app/config.yaml:ro
      - ./data/agent3:/app/data
      - ./certs:/app/certs:ro
    networks:
      - mesh

networks:
  mesh:
    driver: bridge
```

### With Health Checks

```yaml
services:
  agent:
    image: muti-metroo
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
```

## Configuration for Docker

### Agent Config

```yaml
agent:
  id: "auto"
  display_name: "${HOSTNAME:-docker-agent}"
  data_dir: "/app/data"
  log_level: "${LOG_LEVEL:-info}"
  log_format: "json"

tls:
  ca: "/app/certs/ca.crt"
  cert: "/app/certs/agent.crt"
  key: "/app/certs/agent.key"
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

### Docker-Internal Networking

Use container names for peer addresses:

```yaml
# agent1.yaml - uses global TLS settings
peers:
  - id: "${AGENT2_ID}"
    transport: quic
    address: "agent2:4433"
    # Uses global CA and cert/key

# agent2.yaml
peers:
  - id: "${AGENT3_ID}"
    transport: quic
    address: "agent3:4433"
    # Uses global CA and cert/key
```

## Docker Commands

### Start

```bash
# Start all agents
docker compose up -d

# Start specific agents
docker compose up -d agent1 agent2

# View logs
docker compose logs -f agent1

# Stop all
docker compose down
```

### Inspect and Debug

```bash
# Container status
docker compose ps

# Enter container shell
docker compose exec agent1 sh

# View agent logs
docker compose logs agent1

# Follow logs
docker compose logs -f --tail=100 agent1

# Check health
docker compose exec agent1 wget -q -O - http://localhost:8080/health
```

### Certificate Management

```bash
# Generate certs on host, mount to container
muti-metroo cert ca --cn "Docker Mesh CA" -o ./certs
muti-metroo cert agent --cn "agent1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs
```

## Production Considerations

### Resource Limits

```yaml
services:
  agent:
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: 1G
        reservations:
          cpus: "0.5"
          memory: 256M
```

### Logging

```yaml
services:
  agent:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

### Secrets Management

```yaml
services:
  agent:
    secrets:
      - tls_cert
      - tls_key
      - ca_cert

secrets:
  tls_cert:
    file: ./certs/agent.crt
  tls_key:
    file: ./certs/agent.key
  ca_cert:
    file: ./certs/ca.crt
```

### Networking

```yaml
services:
  agent:
    network_mode: host # For better UDP performance (QUIC)
```

Or use bridge with explicit port mapping:

```yaml
services:
  agent:
    ports:
      - "4433:4433/udp"
    networks:
      - mesh
```

## Troubleshooting

### Container Won't Start

```bash
# Check logs
docker compose logs agent1

# Check docker compose config syntax
docker compose config

# Test agent starts successfully (will exit after init)
docker compose run --rm agent1 timeout 5 ./muti-metroo run -c /app/config.yaml || true
```

### Network Issues

```bash
# Check port bindings
docker compose ps

# Test internal connectivity
docker compose exec agent1 nc -zv agent2 4433

# Check DNS resolution
docker compose exec agent1 nslookup agent2
```

### Permission Issues

```bash
# Check file permissions
docker compose exec agent1 ls -la /app/

# Fix data directory permissions
docker compose exec agent1 chown -R 1000:1000 /app/data
```

## Next Steps

- [System Service](/deployment/system-service) - Native installation
- [Embedded Config](/deployment/embedded-config) - Single-file deployment
