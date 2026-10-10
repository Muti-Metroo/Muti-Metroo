---
title: Quick Start
sidebar_position: 4
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-escalator.png" alt="Mole on the move" style={{maxWidth: '180px'}} />
</div>

# Quick Start

By the end of this guide, you will have a working SOCKS5 proxy that can tunnel traffic to any destination. You will be able to run commands like:

```bash
curl -x socks5://localhost:1080 https://internal.example.com
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host
```

This guide gives you full control over the configuration. For a guided experience, use the [Interactive Setup Wizard](/getting-started/interactive-setup) instead.

:::info No TLS Setup Required
Muti Metroo auto-generates TLS certificates at startup. E2E encryption (X25519 + ChaCha20-Poly1305) secures all traffic. For strict TLS verification, see [TLS Configuration](/configuration/tls-certificates).
:::

## Step 1: Initialize Agent Identity

Each agent needs a unique identity. Create one:

```bash
muti-metroo init -d ./data
```

This generates a 128-bit Agent ID stored in `./data/agent_id`:

```
Agent ID: a1b2c3d4e5f6789012345678901234ab
```

:::tip
Save this Agent ID - you will need it when connecting other agents to this one.
:::

## Step 2: Create Configuration File

Create `config.yaml` with a minimal working configuration:

```yaml
agent:
  data_dir: "./data"

listeners:
  - transport: quic
    address: ":4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"

http:
  enabled: true
  address: ":8080"
```

This creates a fully functional agent that can:
- Accept peer connections on port 4433 (QUIC)
- Provide a SOCKS5 proxy on localhost:1080
- Route traffic to any destination (exit)
- Serve the HTTP API on port 8080

:::tip Minimal Config
All other settings use sensible defaults. See [Configuration Reference](/configuration/overview) for customization.
:::

## Step 3: Run the Agent

Start your agent:

```bash
muti-metroo run -c ./config.yaml
```

You should see output like:

```
INFO  Starting Muti Metroo agent
INFO  Agent ID: a1b2c3d4e5f6789012345678901234ab
INFO  QUIC listener started on 0.0.0.0:4433
INFO  SOCKS5 server started on 127.0.0.1:1080
INFO  HTTP server started on :8080
INFO  Agent ready
```

## Step 4: Verify the Agent

In another terminal, test your agent:

```bash
# Check health
curl http://localhost:8080/health
# Output: OK

# Get detailed status
curl http://localhost:8080/healthz
# Output: {"status":"healthy","running":true,"peer_count":0,"stream_count":0,"route_count":0,"socks5_running":true,"exit_handler_running":true}
```

## Step 5: Test the Proxy

Your agent is now a working SOCKS5 proxy:

```bash
# Test with curl
curl -x socks5://localhost:1080 https://httpbin.org/ip

# Test with SSH
ssh -o ProxyCommand='nc -x localhost:1080 %h %p' user@remote-host
```

Query the dashboard API to see the mesh topology:

```bash
curl http://localhost:8080/api/dashboard | jq
```

## Alternative: Client-Only Configuration

If you want to connect to an **existing** exit agent (instead of running your own exit), use this simpler config:

```yaml
agent:
  data_dir: "./data"

peers:
  - id: "exit-agent-id-here"      # Get this from the exit agent
    transport: quic
    address: "exit.example.com:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

This configuration:
- Connects to a remote exit agent
- No listeners needed (outbound-only)
- No exit config needed (uses remote exit)

Ideal for laptops connecting to a central exit server.

## Optional: Transparent Routing with Mutiauk

Instead of configuring each application to use SOCKS5, you can use **Mutiauk** to transparently route all traffic through the mesh.

:::info Linux Only
Mutiauk requires Linux and root privileges to create the TUN interface.
:::

### Install Mutiauk

```bash
curl -L -o mutiauk __MUTIAUK_RELEASE__/mutiauk-linux-amd64
chmod +x mutiauk
sudo mv mutiauk /usr/local/bin/
```

### Option A: Autoroutes (Recommended)

If your exit agents advertise specific CIDR routes (like `10.0.0.0/8` or `192.168.0.0/16`), Mutiauk can automatically sync them:

```bash
sudo tee /etc/mutiauk/config.yaml > /dev/null << 'EOF'
tun:
  name: tun0
  mtu: 1400
  address: 10.200.200.1/24

socks5:
  server: 127.0.0.1:1080

autoroutes:
  enabled: true
  url: "http://localhost:8080"
  poll_interval: 30s
EOF

sudo mutiauk daemon start
```

Routes are automatically fetched from the Muti Metroo API and applied to the TUN interface.

:::warning Default Routes Not Synced
Autoroutes filters out `0.0.0.0/0` (default route) for safety. If your exit advertises `0.0.0.0/0`, use manual routes instead.
:::

### Option B: Manual Routes

For exit agents advertising `0.0.0.0/0` (like the docker-tryout example), add routes manually:

```bash
sudo tee /etc/mutiauk/config.yaml > /dev/null << 'EOF'
tun:
  name: tun0
  mtu: 1400
  address: 10.200.200.1/24

socks5:
  server: 127.0.0.1:1080

routes:
  # Route all traffic (same as 0.0.0.0/0 but in two halves)
  - destination: 0.0.0.0/1
    enabled: true
  - destination: 128.0.0.0/1
    enabled: true
EOF

sudo mutiauk daemon start
```

### Test Transparent Routing

```bash
# No SOCKS5 flag needed - traffic goes through TUN automatically
curl https://httpbin.org/ip
ping 8.8.8.8
```

See [Mutiauk documentation](/mutiauk) for more options including systemd service installation.

## Next Steps

- [Your First Mesh](/getting-started/first-mesh) - Connect multiple agents
- [Configuration Reference](/configuration/overview) - All configuration options
- [SOCKS5 Proxy](/features/socks5-proxy) - Authentication and advanced usage
- [Exit Routing](/features/exit-routing) - Route configuration and DNS

## Troubleshooting

### Agent won't start

Check for common issues:

```bash
# Check if port is already in use
lsof -i :4433
lsof -i :1080
lsof -i :8080

# Check file permissions
ls -la ./data/
ls -la ./certs/

# Enable debug logging in config.yaml:
# agent:
#   log_level: "debug"
muti-metroo run -c ./config.yaml
```

### Certificate errors (strict TLS only)

If you enabled [strict TLS verification](/configuration/tls-certificates):

```bash
# Verify certificate
muti-metroo cert info ./certs/agent.crt

# Check certificate expiration
openssl x509 -in ./certs/agent.crt -text -noout | grep -A2 "Validity"
```

### SOCKS5 connection refused

- Ensure SOCKS5 is enabled in config
- Check the bind address (use `127.0.0.1` for local-only, `0.0.0.0` for network access)
- Verify firewall rules

See [Troubleshooting](/troubleshooting/common-issues) for more help.
