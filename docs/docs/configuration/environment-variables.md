---
title: Environment Variables
sidebar_position: 8
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole reading environment" style={{maxWidth: '180px'}} />
</div>

# Environment Variable Substitution

Keep secrets out of your config files. Reference environment variables instead of hardcoding passwords, API keys, and deployment-specific values.

**Quick example:**
```yaml
socks5:
  auth:
    users:
      - username: "admin"
        password_hash: "${SOCKS5_PASSWORD_HASH}"  # From environment

agent:
  log_level: "${LOG_LEVEL:-info}"                 # Default if not set
```

## Syntax

### Basic Substitution

```yaml
agent:
  log_level: "${LOG_LEVEL}"
```

If `LOG_LEVEL` is not set and no `:-default` is given, the literal text `${LOG_LEVEL}` is left in place -- it is not substituted, and the agent does not abort. To avoid surprising downstream parse errors, always provide a default with `${VAR:-fallback}` or make sure the variable is exported.

### With Default Value

```yaml
agent:
  log_level: "${LOG_LEVEL:-info}"
```

Uses `info` if `LOG_LEVEL` is not set.

### Examples

```yaml
agent:
  id: "${AGENT_ID:-auto}"
  display_name: "${HOSTNAME:-unknown}"
  data_dir: "${DATA_DIR:-./data}"
  log_level: "${LOG_LEVEL:-info}"

socks5:
  address: "${SOCKS_ADDR:-127.0.0.1:1080}"
  auth:
    users:
      - username: "${SOCKS_USER:-user}"
        password_hash: "${SOCKS_PASS_HASH}"

peers:
  - id: "${PEER_ID}"
    transport: "${PEER_TRANSPORT:-quic}"
    address: "${PEER_ADDR}"
```

## Common Use Cases

### Secrets

Never hardcode secrets in configuration:

```yaml
http:
  token_hash: "${HTTP_TOKEN_HASH}"

socks5:
  auth:
    users:
      - username: "admin"
        password_hash: "${SOCKS5_PASSWORD_HASH}"

management:
  signing_public_key:  "${MESH_SIGNING_PUBLIC_KEY}"
  signing_private_key: "${MESH_SIGNING_PRIVATE_KEY}"  # Operator nodes only
```

:::tip CLI Authentication
The CLI also supports `MUTI_METROO_TOKEN` environment variable for API authentication:
```bash
export MUTI_METROO_TOKEN=my-secret-token
muti-metroo status
```
:::

### TLS Certificates

Inline certificates from environment:

```yaml
tls:
  ca_pem: "${TLS_CA}"
  cert_pem: "${TLS_CERT}"
  key_pem: "${TLS_KEY}"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"
```

### Deployment Configuration

Different values per environment:

```yaml
agent:
  display_name: "${ENVIRONMENT:-dev}-${HOSTNAME}"

http:
  address: "${HTTP_ADDR:-:8080}"

listeners:
  - transport: quic
    address: "${LISTEN_ADDR:-0.0.0.0:4433}"
```

### Dynamic Peer Discovery

```yaml
peers:
  - id: "${PEER_1_ID}"
    address: "${PEER_1_ADDR}"
    transport: quic
    tls:
      ca: "${PEER_1_CA:-/etc/muti-metroo/ca.crt}"

  - id: "${PEER_2_ID}"
    address: "${PEER_2_ADDR}"
    transport: quic
    tls:
      ca: "${PEER_2_CA:-/etc/muti-metroo/ca.crt}"
```

## Running with Environment Variables

### Command Line

```bash
# Single variable
LOG_LEVEL=debug muti-metroo run -c config.yaml

# Multiple variables
SOCKS_ADDR=0.0.0.0:1080 \
LOG_LEVEL=info \
PEER_ID=abc123 \
muti-metroo run -c config.yaml
```

### Environment File

Create `.env` file:

```bash
# .env
LOG_LEVEL=info
SOCKS_ADDR=0.0.0.0:1080
SOCKS5_PASSWORD_HASH=$2a$10$...
PEER_ID=abc123def456789012345678901234ab
PEER_ADDR=192.168.1.10:4433
```

Load and run:

```bash
# Using env command
env $(cat .env | xargs) muti-metroo run -c config.yaml

# Using set -a
set -a && source .env && set +a
muti-metroo run -c config.yaml
```

### Docker

```bash
docker run -d \
  -e LOG_LEVEL=info \
  -e SOCKS_ADDR=0.0.0.0:1080 \
  -e PEER_ID=abc123... \
  -v ./config.yaml:/app/config.yaml \
  muti-metroo
```

Or with env file:

```bash
docker run -d \
  --env-file .env \
  -v ./config.yaml:/app/config.yaml \
  muti-metroo
```

### Docker Compose

```yaml
services:
  agent:
    image: muti-metroo
    environment:
      - LOG_LEVEL=info
      - SOCKS_ADDR=0.0.0.0:1080
    env_file:
      - .env
    volumes:
      - ./config.yaml:/app/config.yaml
```

### systemd

```ini
# /etc/systemd/system/muti-metroo.service
[Service]
Environment="LOG_LEVEL=info"
Environment="SOCKS_ADDR=0.0.0.0:1080"
EnvironmentFile=/etc/muti-metroo/environment
ExecStart=/usr/local/bin/muti-metroo run -c /etc/muti-metroo/config.yaml
```

## Best Practices

### 1. Use Defaults for Non-Secrets

```yaml
# Good - has sensible default
log_level: "${LOG_LEVEL:-info}"

# Bad - will fail if not set
log_level: "${LOG_LEVEL}"
```

### 2. Never Default Secrets

```yaml
# Good - forces explicit configuration
password_hash: "${PASSWORD_HASH}"

# Bad - insecure default
password_hash: "${PASSWORD_HASH:-$2a$10$defaulthash...}"
```

### 3. Document Required Variables

```yaml
# Required environment variables:
# - PEER_ID: Agent ID of peer
# - PEER_ADDR: Address of peer
# - SOCKS5_PASSWORD_HASH: bcrypt hash for SOCKS5 auth
#
# Optional:
# - LOG_LEVEL: Logging level (default: info)
# - SOCKS_ADDR: SOCKS5 bind address (default: 127.0.0.1:1080)
```

### 4. Use Descriptive Names

```yaml
# Good
MUTI_METROO_LOG_LEVEL
MUTI_METROO_SOCKS5_PASSWORD_HASH

# OK for simple deployments
LOG_LEVEL
SOCKS5_PASSWORD_HASH
```

### 5. Validate Before Deployment

```bash
# Check all required vars are set
required_vars="PEER_ID PEER_ADDR SOCKS5_PASSWORD_HASH"
for var in $required_vars; do
  if [ -z "${!var}" ]; then
    echo "Error: $var is not set"
    exit 1
  fi
done
```

## Troubleshooting

### Missing Environment Variable

An unset variable with no `:-default` is **not** an error in itself: the literal
`${VAR}` text is passed through to the field unchanged. The failure usually
surfaces later as a confusing downstream error (for example an invalid address,
a never-matching token hash, or a YAML value that looks like `${PEER_ID}`). If a
config value appears to contain a literal `${...}`, the variable was unset.

Set the variable (export it before launching the agent) or add a default value
with `${VAR:-fallback}`.

### Multiline Values

For multiline values (like PEM certificates):

```bash
# In shell
export TLS_CERT="$(cat ./certs/agent.crt)"

# In .env file (escape newlines)
TLS_CERT="-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----"
```

### Special Characters

Escape special characters in values:

```bash
# Password with special chars
SOCKS5_PASSWORD_HASH='$2a$10$abc123...'  # Use single quotes
```

## Related

- [Configuration Overview](/configuration/overview) - Full configuration reference
- [Deployment: Docker](/deployment/docker) - Docker deployment
