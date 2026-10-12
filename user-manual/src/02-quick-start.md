# Quick Start

Get Muti Metroo running in under 5 minutes using the interactive setup wizard.

## Get the Binary

Obtain the binary for your platform from the distribution channel your team uses. Pick the file that matches your OS and CPU:

| Platform              | Filename             |
| --------------------- | -------------------- |
| Linux (amd64)         | `muti-metroo-linux-amd64`  |
| Linux (arm64)         | `muti-metroo-linux-arm64`  |
| macOS (Apple Silicon) | `muti-metroo-darwin-arm64` |
| macOS (Intel)         | `muti-metroo-darwin-amd64` |

Then install it (example: Linux amd64):

```bash
chmod +x muti-metroo-linux-amd64
sudo mv muti-metroo-linux-amd64 /usr/local/bin/muti-metroo

# Verify installation
muti-metroo --version
```

## Run the Setup Wizard

The easiest way to get started is using the interactive setup wizard:

```bash
muti-metroo setup
```

The wizard guides you through 11 numbered steps (some are skipped based on
the role you selected):

1. **Basic Setup** - Data directory and config file location (identity is
   auto-generated and never prompted for)
2. **Agent Role** - Ingress, Transit, Exit, or any combination
3. **Network Configuration** - Transport listeners (QUIC, HTTP/2, WebSocket)
4. **TLS Configuration** - Auto-generate self-signed or use existing certs
5. **Peer Connections** - Static peers to dial on startup
6. **SOCKS5 Proxy** - Ingress proxy settings (skipped if not ingress)
7. **Exit Configuration** - CIDR/domain routes (skipped if not exit)
8. **Monitoring & Logging** - HTTP API server and bearer-token auth
9. **Management Commands** - run commands via signed management envelopes
10. **File Transfer** - Upload/download with signed management envelopes
11. **Management Key Encryption** - X25519 topology key + Ed25519 signing key

The wizard ends with a configuration-delivery prompt (write a `config.yaml`
or build an embedded self-contained binary) and an optional service-install
step.

## Start the Agent

After the wizard completes:

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

## Verify Operation

In another terminal:

```bash
# Check health
curl http://localhost:8080/health
# Output: OK

# Get detailed status (JSON)
curl http://localhost:8080/healthz
# Returns: status, running, peer_count, stream_count, route_count,
#          socks5_running, exit_handler_running,
#          can_sign_management, accepts_signed_management
```

## Test SOCKS5 Proxy

If you configured exit routing (standalone mode):

```bash
# Test the proxy
curl -x socks5://localhost:1080 https://example.com

# Test with authentication (if configured)
curl -x socks5://user:password@localhost:1080 https://example.com
```

## Minimal Configuration Example

For reference, here is a minimal standalone configuration (ingress + exit):

> **Note:** TLS certificates are auto-generated. E2E encryption secures all traffic by default.

```yaml
agent:
  id: "auto"
  display_name: "Standalone Agent"
  data_dir: "./data"
  log_level: "info"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  # dns.servers defaults to [] (use system resolver, including .local / mDNS).
  # Override only when you need to pin specific upstreams. Do NOT use public
  # resolvers (8.8.8.8, 1.1.1.1) in compliance-bound deployments -- they
  # leak DNS to a third party.

http:
  enabled: true
  address: ":8080"
```

## Next Steps

- **Chapter 3**: Detailed installation options
- **Chapter 4**: Setup wizard walkthrough
- **Chapter 5**: TLS certificate management
- **Chapter 6**: Full configuration reference
