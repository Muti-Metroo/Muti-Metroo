<p align="center">
  <img src="docs/static/img/logo.png" alt="Muti Metroo" width="200">
</p>

# Muti Metroo

A userspace mesh networking agent that builds VPN-like overlay networks across heterogeneous transport layers. It enables multi-hop routing with SOCKS5 ingress and CIDR-based exit routing, operating entirely in unprivileged userspace - no root privileges, no kernel extensions, no drivers.

## Features

- **Multiple Transport Layers**: QUIC/TLS 1.3, HTTP/2, and WebSocket transports
- **SOCKS Proxy Ingress**: Accept SOCKS4, SOCKS4a, and SOCKS5 client connections with optional username/password authentication (TCP or WebSocket transport)
- **Port Forwarding**: Reverse tunnel to expose local services through the mesh (ngrok-style)
- **CIDR-Based Exit Routing**: Advertise routes and handle DNS resolution at exit nodes
- **Multi-Hop Mesh Routing**: Flood-based route propagation with longest-prefix match
- **Stream Multiplexing**: Multiple virtual streams over a single peer connection with half-close support
- **Automatic Reconnection**: Exponential backoff with jitter for resilient peer connections
- **No Root Required**: Runs entirely in userspace - no kernel extensions or drivers
- **Mesh Encryption**: X25519 + ChaCha20-Poly1305 between ingress and exit (transit cannot decrypt)
- **Embedded Configuration**: Single-file deployment with config baked into binary

## How It Works

```
                                       MESH NETWORK
                         .---------------------------------------------.
    +--------+     +-------------+     +-------------+     +-------------+     +--------+
    |        |     |   Agent A   |     |   Agent B   |     |   Agent C   |     |        |
    | Client |---->|   INGRESS   |=====|   TRANSIT   |=====|    EXIT     |---->| Target |
    |        |     |             |     |             |     |             |     | Server |
    +--------+     +-------------+     +-------------+     +-------------+     +--------+
                         |                                       |
        SOCKS5           '-----------Encrypted Tunnel------------'           TCP
        Proxy                    (Transit Cannot Decrypt)                 Connection
```

**Traffic Flow:**

1. Client connects to SOCKS5 proxy on the ingress agent
2. Ingress looks up route, finds path to exit agent through transit nodes
3. Virtual stream opens with encryption between ingress and exit
4. Exit agent opens real TCP connection to target server
5. Data flows bidirectionally - encrypted within the mesh, plain at endpoints

## Transparent TUN Routing with Mutiauk

For transparent traffic routing without per-application SOCKS5 configuration, use **[Mutiauk](https://github.com/postalsys/Mutiauk)** - a companion TUN interface tool that forwards all traffic through Muti Metroo's SOCKS5 proxy.

```
    OPERATOR LINUX                                 MESH NETWORK
    .-------------------.          .---------------------------------------------.
    |    +-----------+  |    +-------------+     +-------------+     +-------------+     +--------+
    |    |    App    |  |    |   Agent A   |     |   Agent B   |     |   Agent C   |     |        |
    |    +-----------+  |    |   INGRESS   |=====|   TRANSIT   |=====|    EXIT     |---->| Target |
    |          |        |    |             |     |             |     |             |     | Server |
    |    +-----------+  |    +-------------+     +-------------+     +-------------+     +--------+
    |    |  Mutiauk  |------>|  SOCKS5     |           |                   |
    |    |   (TUN)   |  |    |  :1080      |           |                   |
    |    +-----------+  |    +-------------+           |                   |
    '-------------------'          '---------Encrypted Tunnel--------------'
      No proxy config                      (Transit Cannot Decrypt)
        required
```

**With Mutiauk** (transparent routing):

1. Application sends traffic normally (no proxy configuration needed)
2. Mutiauk TUN interface captures traffic destined for configured routes
3. Traffic is forwarded to Muti Metroo's SOCKS5 proxy on localhost
4. Mesh routing and encryption work the same as direct SOCKS5 usage

```bash
# Install Mutiauk (Linux only)
curl -L -o mutiauk https://github.com/postalsys/Mutiauk/releases/latest/download/mutiauk-linux-amd64
chmod +x mutiauk && sudo mv mutiauk /usr/local/bin/

# Run interactive setup
sudo mutiauk setup

# Or manual configuration
sudo mutiauk daemon start
```

With Mutiauk, any application's traffic to configured routes is automatically tunneled through the mesh - no proxy settings required. Key advantages:

- **Native multi-hop routing** - no manual listener chaining for double pivots
- **End-to-end encryption** - transit nodes cannot decrypt traffic
- **Multiple transports** - QUIC, HTTP/2, WebSocket vs TCP-only

See the [Mutiauk repository](https://github.com/postalsys/Mutiauk) for detailed setup instructions.

## Documentation

| Resource                                                             | Description                                                                          |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| **[Documentation](https://github.com/postalsys/Muti-Metroo)**              | Online documentation with installation, configuration, and usage guides              |
| **[User Manual (PDF)](https://github.com/postalsys/Muti-Metroo/releases)** | Comprehensive PDF manual for offline use and printing (download from latest release) |
| **[Architecture.md](./Architecture.md)**                             | Technical internals, protocol specification, and implementation details              |

## Quick Start

### Prerequisites

- Make (optional, for convenience commands)

### Build

```bash
# Clone the repository
git clone git@github.com:postalsys/Muti-Metroo.git
cd Muti-Metroo

# Build the binary
make build

# Or without make:
go build -o build/muti-metroo ./cmd/muti-metroo
```

### Interactive Setup (Recommended)

The easiest way to get started is using the interactive setup wizard:

```bash
./build/muti-metroo setup
```

The wizard guides you through:

- **Basic setup**: Data directory and config file location
- **Agent role**: Ingress (SOCKS5 proxy), Transit (relay), or Exit (external network access)
- **Network config**: Transport protocol (QUIC/HTTP2/WebSocket) and listen address
- **TLS certificates**: Generate new, paste existing, or use certificate files
- **Peer connections**: Connect to other mesh agents
- **SOCKS5 settings**: Configure proxy authentication (for ingress nodes)
- **Exit routes**: Define allowed destination networks (for exit nodes)
- **Advanced options**: Logging, health checks, control socket
- **Config delivery**: Save to file or embed in binary (single-file deployment)
- **Service installation**: Install as system service (Linux/macOS/Windows, requires root/admin)

The wizard generates a complete `config.yaml` and initializes your agent identity.

### Embedded Configuration

For single-file deployments, the wizard can embed configuration directly into the binary:

```bash
# Create embedded binary during setup
./build/muti-metroo setup
# Choose "Embed in binary" when prompted

# Run without config file (no 'run' command needed when
# the embedded config sets default_action: run)
./my-agent

# Edit embedded config later
./build/muti-metroo setup -c /path/to/embedded-binary
```

This simplifies deployment and distribution.

### Service Installation

When running the setup wizard as root (Linux/macOS) or Administrator (Windows), you'll be offered to install Muti Metroo as a system service:

- **Linux**: Creates a systemd service that starts on boot
- **macOS**: Creates a launchd daemon that starts on boot
- **Windows**: Registers a Windows service that starts automatically

To uninstall the service:

```bash
# Linux/macOS (as root)
sudo muti-metroo service uninstall

# Windows (as Administrator)
muti-metroo service uninstall
```

Service management commands:

```bash
# Linux (systemd)
systemctl status muti-metroo    # Check status
journalctl -u muti-metroo -f    # View logs
systemctl restart muti-metroo   # Restart

# macOS (launchd)
launchctl list com.muti-metroo  # Check status
tail -f /path/to/working/dir/muti-metroo.log  # View logs
launchctl stop com.muti-metroo  # Stop
launchctl start com.muti-metroo # Start

# Windows
sc query muti-metroo            # Check status
net start muti-metroo           # Start
net stop muti-metroo            # Stop
```

### Manual Setup

If you prefer manual configuration:

### Initialize Agent

```bash
# Initialize agent identity
./build/muti-metroo init -d ./data
```

### Generate Certificates

Generate fresh TLS certificates for your deployment:

```bash
# Generate CA certificate (valid for 1 year)
./build/muti-metroo cert ca -n "My Mesh CA" -o ./certs

# Generate agent certificates signed by the CA
./build/muti-metroo cert agent -n "agent-a" --dns "node-a.example.com" --ip "192.168.1.10"
./build/muti-metroo cert agent -n "agent-b" --dns "node-b.example.com" --ip "192.168.1.20"

# Generate client certificate (for mTLS)
./build/muti-metroo cert client -n "admin-client"

# View certificate details
./build/muti-metroo cert info ./certs/agent-a.crt
```

Certificate commands:

- `cert ca` - Generate a new Certificate Authority
- `cert agent` - Generate an agent/peer certificate (server + client auth)
- `cert client` - Generate a client-only certificate
- `cert info` - Display certificate information and expiration status

### Run

```bash
# Copy and edit the example configuration
cp configs/example.yaml config.yaml
# Edit config.yaml with your settings

# Run the agent
./build/muti-metroo run -c ./config.yaml

# Or with make:
make run

# Windows: keep running after SSH disconnect (re-spawns outside session Job Object)
muti-metroo.exe run --detach -c config.yaml
```

## Configuration

See `configs/example.yaml` for a fully commented configuration file. Key sections:

### Agent Identity

```yaml
agent:
  id: "auto" # Auto-generate on first run, or specify hex string
  display_name: "" # Human-readable name (Unicode allowed)
  data_dir: "./data" # Directory for persistent state
  log_level: "info" # debug, info, warn, error
  log_format: "text" # text, json
```

### Transport Listeners

```yaml
listeners:
  - transport: quic # quic, h2, or ws
    address: "0.0.0.0:4433"
    tls:
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"
```

### Peer Connections

```yaml
peers:
  - id: "abc123..." # Expected peer AgentID
    transport: quic
    address: "192.168.1.50:4433"
    tls:
      ca: "./certs/peer-ca.crt"
```

### SOCKS5 Server (Ingress)

```yaml
socks5:
  enabled: true
  address: "127.0.0.1:1080"
  auth:
    enabled: false
    users:
      - username: "user1"
        password_hash: "$2a$10$..." # bcrypt hash (use: muti-metroo hash)
  # WebSocket transport (optional, for firewall bypass)
  # When auth.enabled is true, WebSocket requires HTTP Basic Auth with same credentials
  websocket:
    enabled: false
    address: "0.0.0.0:8443"
    path: "/socks5"
    plaintext: false # true when behind reverse proxy
```

### Exit Node

```yaml
exit:
  enabled: true
  routes:
    - "10.0.0.0/8"
    - "0.0.0.0/0" # Default route
  dns:
    servers:
      - "8.8.8.8:53"
    timeout: 5s
```

## Architecture

### Agent Roles

An agent can serve multiple roles simultaneously:

- **Ingress**: SOCKS5 listener, initiates virtual streams, performs route lookup
- **Transit**: Forwards streams between peers, participates in route flooding
- **Exit**: Opens real TCP connections, advertises CIDR routes, handles DNS

### Data Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │     │   Agent A   │     │   Agent B   │     │   Agent C   │
│  (Browser)  │     │  (Ingress)  │     │  (Transit)  │     │   (Exit)    │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │                   │
       │ SOCKS5 CONNECT    │                   │                   │
       │──────────────────>│                   │                   │
       │                   │                   │                   │
       │                   │ Route Lookup      │                   │
       │                   │ (longest match)   │                   │
       │                   │                   │                   │
       │                   │ STREAM_OPEN       │ STREAM_OPEN       │
       │                   │──────────────────>│──────────────────>│
       │                   │                   │                   │
       │                   │                   │                   │ TCP Connect
       │                   │                   │                   │────────────>
       │                   │                   │                   │
       │                   │ STREAM_OPEN_ACK   │ STREAM_OPEN_ACK   │
       │                   │<──────────────────│<──────────────────│
       │                   │                   │                   │
       │ SOCKS5 OK         │                   │                   │
       │<──────────────────│                   │                   │
       │                   │                   │                   │
       │     STREAM_DATA frames (bidirectional relay)              │
       │<═══════════════════════════════════════════════════════════>
```

### Package Structure

| Package        | Purpose                                                                                                                                                                       |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent`        | Main orchestrator - initializes components, dispatches frames, manages lifecycle                                                                                              |
| `apiclient`    | HTTP client used by the CLI to call local and remote agent APIs (token auth, signed envelopes)                                                                                |
| `certutil`     | TLS certificate generation and management - CA, server, client, peer certs                                                                                                    |
| `chaos`        | Chaos testing utilities - fault injection, ChaosMonkey for resilience testing                                                                                                 |
| `config`       | YAML config parsing with env var substitution (`${VAR:-default}`)                                                                                                             |
| `crypto`       | End-to-end encryption - X25519 key exchange, ChaCha20-Poly1305, session key derivation                                                                                        |
| `dynconfig`    | Append-only JSONL journal persisting runtime peer/forward/route/display-name mutations                                                                                        |
| `embed`        | Embedded configuration - XOR encoding, binary config extraction and appending                                                                                                 |
| `exit`         | Exit node handler - TCP dial, route-based access control, E2E decryption                                                                                                      |
| `expose`       | Ephemeral forward endpoints over SOCKS5 - operators register ephemeral forward keys over the SOCKS5 `CmdExpose` stream; the registration disappears when the connection drops |
| `filetransfer` | Streaming file/directory transfer with tar, gzip, and permission preservation                                                                                                 |
| `flood`        | Route propagation via flooding with loop prevention and seen-cache                                                                                                            |
| `forward`      | Port forwarding (reverse tunnel) - endpoints expose local services, listeners accept remote                                                                                   |
| `health`       | Health check HTTP server, remote agent status, pprof, dashboard, mesh connectivity testing                                                                                    |
| `hosts`        | Mesh-local hostname registry - config + journalled runtime entries                                                                                                            |
| `icmp`         | ICMP echo (ping) - exit handler, unprivileged sockets, session management with E2E encryption                                                                                 |
| `identity`     | 128-bit AgentID generation, X25519 keypair storage for E2E encryption                                                                                                         |
| `integration`  | Integration tests for multi-agent mesh scenarios                                                                                                                              |
| `loadtest`     | Load testing utilities - stream throughput, route table, connection churn                                                                                                     |
| `logging`      | Structured logging with slog - text/JSON formats, standard attribute keys                                                                                                     |
| `peer`         | Peer connection lifecycle - handshake, keepalive, reconnection with backoff                                                                                                   |
| `probe`        | Connectivity testing for Muti Metroo listeners - transport dial, handshake verification                                                                                             |
| `protocol`     | Binary frame protocol - 14-byte header, encode/decode for all frame types                                                                                                     |
| `recovery`     | Panic recovery utilities for goroutines with logging and callbacks                                                                                                            |
| `routing`      | Route table with CIDR longest-prefix match, domain pattern matching, agent presence table, route manager                                                                      |
| `service`      | Cross-platform service management - systemd (Linux), launchd (macOS), Windows Service                                                                                         |
| `shell`        | Management commands - interactive (PTY) and streaming command execution, whitelist, authentication                                                                            |
| `sleep`        | Mesh hibernation - state machine, jittered polling, command deduplication, persistence, deterministic windows                                                                 |
| `socks5`       | SOCKS proxy server (SOCKS4/4a/5 auto-detected) with no-auth and username/password methods, optional WebSocket transport                                                       |
| `stream`       | Stream state machine (Opening->Open->HalfClosed->Closed), buffered I/O                                                                                                        |
| `sysinfo`      | System information collection for node info advertisements                                                                                                                    |
| `transport`    | Transport abstraction with QUIC, HTTP/2, and WebSocket implementations, TLS fingerprinting                                                                                    |
| `udp`          | UDP relay handler for SOCKS5 UDP ASSOCIATE - association lifecycle, datagram forwarding                                                                                       |
| `wizard`       | Interactive setup wizard with certificate generation                                                                                                                          |

## Development

### Build Commands

```bash
make build            # Build binary to ./build/muti-metroo
make test             # Run all tests with race detection
make test-coverage    # Generate coverage report
make test-short       # Run short tests only
make lint             # Run gofmt and go vet
make fmt              # Format code
make clean            # Clean build artifacts
make deps             # Download and tidy dependencies
```

### Running Tests

```bash
# All tests
make test

# Specific package
go test -v ./internal/transport/...

# Single test
go test -v -run TestName ./internal/peer/

# Integration tests
go test -v ./internal/integration/...
```

### Docker

```bash
# Build image
docker build -t muti-metroo .

# Run with config
docker run -v $(pwd)/config.yaml:/app/config.yaml \
           -v $(pwd)/data:/app/data \
           -v $(pwd)/certs:/app/certs \
           -p 1080:1080 -p 4433:4433/udp \
           muti-metroo
```

## Protocol Details

- **Frame Size**: Max 16 KB payload
- **Buffer Size**: 256 KB per stream default
- **Timeouts**: Handshake 10s, Stream open 30s, Idle stream 5m
- **Keepalive**: Configurable idle threshold (default 5m), 90s timeout
- **Reconnection**: Exponential backoff 1s->60s with 20% jitter
- **Protocol Version**: 0x01

### Frame Types

| Type | Name                | Description                              |
| ---- | ------------------- | ---------------------------------------- |
| 0x01 | STREAM_OPEN         | Open a new virtual stream                |
| 0x02 | STREAM_OPEN_ACK     | Stream opened successfully               |
| 0x03 | STREAM_OPEN_ERR     | Stream open failed                       |
| 0x04 | STREAM_DATA         | Stream data payload                      |
| 0x05 | STREAM_CLOSE        | Close stream                             |
| 0x06 | STREAM_RESET        | Reset stream with error                  |
| 0x10 | ROUTE_ADVERTISE     | Announce CIDR routes                     |
| 0x11 | ROUTE_WITHDRAW      | Remove CIDR routes                       |
| 0x12 | NODE_INFO_ADVERTISE | Announce node metadata                   |
| 0x20 | PEER_HELLO          | Handshake initiation                     |
| 0x21 | PEER_HELLO_ACK      | Handshake acknowledgment                 |
| 0x22 | KEEPALIVE           | Connection keepalive                     |
| 0x23 | KEEPALIVE_ACK       | Keepalive acknowledgment                 |
| 0x24 | CONTROL_REQUEST     | Request metrics/status from remote agent |
| 0x25 | CONTROL_RESPONSE    | Response with metrics/status data        |

## License

MIT License - see [LICENSE](./LICENSE) file.
