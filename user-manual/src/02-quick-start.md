# Quick Start

This chapter starts a single agent with the interactive setup wizard and tests it with a SOCKS5 client. In a real deployment agents run on the mesh nodes (relays, gateways, exits), and the operator's workstation reaches the mesh through Mutiauk; see "Connect from Your Workstation" at the end of this chapter.

## Get the Binary

Obtain the binary for your platform from the distribution channel your team uses. Pick the file that matches your OS and CPU:

| Platform              | Filename             |
| --------------------- | -------------------- |
| Linux (amd64)         | `muti-metroo-linux-amd64`  |
| Linux (arm64)         | `muti-metroo-linux-arm64`  |
| macOS (Apple Silicon) | `muti-metroo-darwin-arm64` |
| macOS (Intel)         | `muti-metroo-darwin-amd64` |
| Windows (amd64)       | `muti-metroo-windows-amd64.exe` |
| Windows (arm64)       | `muti-metroo-windows-arm64.exe` |

Then install it (example: Linux amd64):

```bash
chmod +x muti-metroo-linux-amd64
sudo mv muti-metroo-linux-amd64 /usr/local/bin/muti-metroo

# Verify installation
muti-metroo --version
```

## Run the Setup Wizard

Start with the interactive setup wizard:

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

## Quick Test with a SOCKS5 Client

If you configured exit routing (standalone mode), point `curl` at the SOCKS5 listener. This direct SOCKS5 path is useful for quick tests and for single applications:

```bash
# Test the proxy (socks5h sends the hostname to the agent for resolution)
curl -x socks5h://localhost:1080 https://example.com

# Test with authentication (if configured)
curl -x socks5h://user:password@localhost:1080 https://example.com
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
  # resolvers (8.8.8.8, 1.1.1.1) in compliance-bound deployments; they
  # leak DNS to a third party.

http:
  enabled: true
  address: ":8080"
```

## Connect from Your Workstation

The usual operator setup runs the ingress agent on a mesh node, for example a relay on a public server, and runs Mutiauk on the workstation:

1. On the ingress agent, enable SOCKS5 authentication and the SOCKS5 WebSocket listener with a TLS certificate the workstation trusts (Chapter 9).
2. On the workstation, create a Mutiauk configuration whose `socks5.server` points at the agent, for example `wss://relay.example.com:8443/socks5`, with `autoroutes.enabled: true`, and start the Mutiauk daemon (Chapter 10). The Mutiauk config wizard in Muti Metroo Manager and `mutiauk config generate` both accept a `wss://` server; the interactive `mutiauk setup` wizard asks for a plain `host:port`.
3. Optionally run Muti Metroo Manager on the workstation against an agent's HTTP API for the topology view and management actions (Chapter 18).

After that, ordinary tools reach mesh networks with no proxy settings:

```bash
ssh admin@10.10.5.100
curl http://10.10.5.100/
```

## Next Steps

- **Chapter 3**: Detailed installation options
- **Chapter 4**: Setup wizard walkthrough
- **Chapter 5**: TLS certificate management
- **Chapter 6**: Full configuration reference
- **Chapter 9**: SOCKS5 ingress listener
- **Chapter 10**: Mutiauk TUN interface on the operator workstation
