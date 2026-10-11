# Mesh Agent Network Architecture

**Version:** 2.6
**Date:** 2026-04-16

## Executive Overview

Muti Metroo is a userspace mesh networking agent that builds VPN-like overlay networks across heterogeneous transport layers (QUIC, HTTP/2, WebSocket). Agents form a mesh network where each can serve as ingress (SOCKS5 proxy entry point), transit (relay between networks), or exit (connection to target destinations). Traffic flows through multi-hop paths with automatic route discovery via flood-based propagation.

The architecture provides end-to-end encryption using X25519 key exchange and ChaCha20-Poly1305, ensuring transit agents cannot decrypt payload data. Stream multiplexing enables concurrent connections with full TCP semantics including half-close support. The system operates entirely in unprivileged userspace - no root privileges, no kernel extensions, no TUN driver - making it suitable for deployment across diverse environments where traditional VPNs are impractical.

---

## Table of Contents

- [Executive Overview](#executive-overview)

1. [Executive Summary](#1-executive-summary)
2. [System Overview](#2-system-overview)
3. [Core Components](#3-core-components)
4. [Identity and Addressing](#4-identity-and-addressing)
5. [Transport Layer](#5-transport-layer)
6. [Frame Protocol](#6-frame-protocol)
   - [6.1 Frame Format](#61-frame-format)
   - [6.2 Frame Types](#62-frame-types)
   - [6.3 Frame Flags](#63-frame-flags)
   - [6.4 Payload Definitions](#64-payload-definitions)
     - [6.4.1 Session Key Derivation](#641-session-key-derivation)
     - [6.4.2 Protocol Evolution Policy](#642-protocol-evolution-policy)
   - [6.5 UDP Relay Protocol](#65-udp-relay-protocol)
   - [6.6 Port Forwarding](#66-port-forwarding-reverse-tunnel)
   - [6.7 ICMP Echo Protocol](#67-icmp-echo-protocol)
   - [6.8 Sleep Mode Protocol](#68-sleep-mode-protocol)
7. [Stream Management](#7-stream-management)
8. [Routing System](#8-routing-system)
9. [Flood Protocol](#9-flood-protocol)
10. [Peer Connection Management](#10-peer-connection-management)
11. [SOCKS5 Server](#11-socks5-server)
12. [Data Plane](#12-data-plane)
13. [Configuration](#13-configuration)
14. [Security](#14-security)
15. [Observability](#15-observability)
16. [Operations](#16-operations)
17. [Certificate Management](#17-certificate-management)
18. [Project Structure](#18-project-structure)
19. [Implementation Notes](#19-implementation-notes)
20. [Testing Strategy](#20-testing-strategy)

- [Appendix A: Quick Reference](#appendix-a-quick-reference)
- [Appendix B: Mutiauk - Optional TUN Interface](#appendix-b-mutiauk---optional-tun-interface)

---

## 1. Executive Summary

### 1.1 Purpose

This document describes the architecture for a userspace mesh networking agent that creates virtual TCP tunnels across heterogeneous transport layers. The system enables multi-hop routing with SOCKS5 ingress and CIDR-based exit routing, operating entirely in userspace without requiring root privileges.

### 1.2 Design Goals

| Goal                      | Description                                                 |
| ------------------------- | ----------------------------------------------------------- |
| **Userspace operation**   | No kernel modules, no root/admin privileges required        |
| **Transport flexibility** | Support QUIC, HTTP/2, and WebSocket to traverse any network |
| **Multi-hop routing**     | Chain agents across network boundaries                      |
| **Bidirectional streams** | Full TCP semantics including half-close                     |
| **Automatic recovery**    | Reconnect on failure, re-advertise routes                   |
| **Low latency**           | Suitable for interactive applications (SSH)                 |
| **High throughput**       | Capable of streaming video                                  |
| **Production ready**      | Health checks, service management                           |

### 1.3 Target Scale

This architecture targets small to medium deployments:

- **Agents:** Up to 20
- **Concurrent users:** Up to 10
- **Traffic types:** Interactive (SSH), streaming (video), bulk transfers
- **Typical topology:** Chains of 2-5 agents with occasional branching

The design prioritizes simplicity and correctness over extreme scalability.

### 1.4 Implementation Language

The reference implementation is written in **Go**, leveraging its excellent concurrency primitives and networking libraries.

---

## 2. System Overview

### 2.1 Network Topology

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              MESH NETWORK                                   │
│                                                                             │
│  ┌─────────┐      QUIC       ┌─────────┐     HTTP/2     ┌─────────┐         │
│  │ Agent 1 │◄───────────────►│ Agent 2 │◄──────────────►│ Agent 3 │         │
│  │         │    (UDP/4433)   │         │   (TCP/8443)   │         │         │
│  │ Entry   │                 │ Transit │                │  Exit   │         │
│  │ SOCKS5  │                 │         │                │ 10.0.0.0/8        │
│  │ :1080   │                 │         │                │         │         │
│  └────▲────┘                 └─────────┘                └────┬────┘         │
│       │                                                      │              │
│       │ Client                                          Real │ TCP          │
│       │ Application                                          │              │
│  ┌────┴────┐                                           ┌─────▼─────┐        │
│  │  curl   │                                           │  Target   │        │
│  │ Browser │                                           │  Server   │        │
│  │  SSH    │                                           │ 10.5.3.100│        │
│  └─────────┘                                           └───────────┘        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Agent Roles

An agent can serve one or more roles simultaneously:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AGENT ROLES                                    │
│                                                                             │
│  ┌───────────────────┐  ┌───────────────────┐  ┌───────────────────┐        │
│  │      INGRESS      │  │      TRANSIT      │  │       EXIT        │        │
│  │                   │  │                   │  │                   │        │
│  │ • SOCKS5 listener │  │ • Forward streams │  │ • Open real TCP   │        │
│  │ • Initiates       │  │ • Route flooding  │  │ • Advertise CIDRs │        │
│  │   virtual streams │  │ • No local I/O    │  │ • DNS resolution  │        │
│  │ • Route lookup    │  │                   │  │                   │        │
│  └───────────────────┘  └───────────────────┘  └───────────────────┘        │
│                                                                             │
│  Deployment Examples:                                                       │
│  • Home laptop:     Ingress only (SOCKS5 for browser)                       │
│  • Cloud relay:     Transit only (forward between networks)                 │
│  • Office gateway:  Exit only (access to internal network)                  │
│  • VPN endpoint:    Ingress + Exit (full VPN replacement)                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.3 Traffic Flow Example

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         END-TO-END TRAFFIC FLOW                             │
│                                                                             │
│  1. User runs: ssh -o ProxyCommand='nc -x localhost:1080 %h %p' server      │
│                                                                             │
│  2. SSH connects to SOCKS5 proxy on Agent1 (localhost:1080)                 │
│                                                                             │
│  3. SOCKS5 receives: CONNECT server.internal:22                             │
│                                                                             │
│  4. Agent1 looks up route for "server.internal"                             │
│     Result: Origin=Agent3, Path=[Agent2, Agent3]                            │
│                                                                             │
│  5. Agent1 sends STREAM_OPEN to Agent2:                                     │
│     - Destination: server.internal:22                                       │
│     - RemainingPath: [Agent3]                                               │
│                                                                             │
│  6. Agent2 forwards STREAM_OPEN to Agent3:                                  │
│     - RemainingPath: [] (empty = I am exit)                                 │
│                                                                             │
│  7. Agent3 resolves "server.internal" via local DNS                         │
│     Opens TCP connection to server.internal:22                              │
│     Sends STREAM_OPEN_ACK back through chain                                │
│                                                                             │
│  8. Agent1 receives ACK, sends SOCKS5 success to SSH                        │
│                                                                             │
│  9. SSH session data flows bidirectionally through the chain                │
│     User ↔ Agent1 ↔ Agent2 ↔ Agent3 ↔ SSH Server                            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Core Components

### 3.1 Component Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AGENT PROCESS                                  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                          CONTROL PLANE                                │  │
│  │                                                                       │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐           │  │
│  │  │  Route Table   │  │ Flood Protocol │  │  Peer Manager  │           │  │
│  │  │                │  │                │  │                │           │  │
│  │  │ • CIDR entries │  │ • Advertise    │  │ • Connections  │           │  │
│  │  │ • LPM lookup   │  │ • Withdraw     │  │ • Reconnection │           │  │
│  │  │ • Path cache   │  │ • Loop prevent │  │ • Handshake    │           │  │
│  │  └────────────────┘  └────────────────┘  └────────────────┘           │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                           DATA PLANE                                  │  │
│  │                                                                       │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐           │  │
│  │  │ Stream Manager │  │ Forward Table  │  │  Exit Handler  │           │  │
│  │  │                │  │                │  │                │           │  │
│  │  │ • Lifecycle    │  │ • Stream map   │  │ • TCP connect  │           │  │
│  │  │ • Half-close   │  │ • Relay data   │  │ • DNS resolve  │           │  │
│  │  │ • Fairness     │  │ • Cleanup      │  │ • Error handle │           │  │
│  │  └────────────────┘  └────────────────┘  └────────────────┘           │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        TRANSPORT LAYER                                │  │
│  │                                                                       │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐           │  │
│  │  │     QUIC       │  │    HTTP/2      │  │   WebSocket    │           │  │
│  │  │    (UDP)       │  │   Streaming    │  │   HTTP/1.1     │           │  │
│  │  │                │  │                │  │                │           │  │
│  │  │ • Native mux   │  │ • Single POST  │  │ • Proxy compat │           │  │
│  │  │ • 0-RTT        │  │ • Bidi stream  │  │ • Upgrade      │           │  │
│  │  │ • Migration    │  │ • Port 443     │  │ • Binary frame │           │  │
│  │  └────────────────┘  └────────────────┘  └────────────────┘           │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                         INGRESS LAYER                                 │  │
│  │                                                                       │  │
│  │  ┌─────────────────────────────────────────────────────────────────┐  │  │
│  │  │                       SOCKS5 Server                             │  │  │
│  │  │  • CONNECT command  • IPv4/IPv6/Domain  • Optional auth         │  │  │
│  │  └─────────────────────────────────────────────────────────────────┘  │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                       OBSERVABILITY LAYER                             │  │
│  │                                                                       │  │
│  │  ┌──────────────────────────────────────────────────┐                 │  │
│  │  │            Health Check / HTTP API               │                 │  │
│  │  │                   (HTTP)                         │                 │  │
│  │  └──────────────────────────────────────────────────┘                 │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 3.2 Component Responsibilities

| Component               | Responsibility                                          |
| ----------------------- | ------------------------------------------------------- |
| **Route Table**         | Store CIDR→Path mappings, perform longest-prefix match  |
| **Flood Protocol**      | Propagate route advertisements between peers            |
| **Peer Manager**        | Manage peer connections, handle reconnection            |
| **Stream Manager**      | Track virtual stream lifecycle and state                |
| **Forward Table**       | Map incoming streams to outgoing destinations           |
| **Exit Handler**        | Open real TCP connections, handle DNS                   |
| **QUIC Transport**      | High-performance UDP transport with native multiplexing |
| **HTTP/2 Transport**    | TCP streaming for direct connections                    |
| **WebSocket Transport** | HTTP/1.1 WebSocket for proxy traversal                  |
| **SOCKS5 Server**       | Accept client connections, initiate streams             |
| **Health Check**        | HTTP endpoints for liveness/readiness probes            |

---

## 4. Identity and Addressing

### 4.1 Agent Identity

Each agent has a persistent unique identifier:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             AGENT IDENTITY                                  │
│                                                                             │
│  AgentID: 16 bytes (128 bits)                                               │
│                                                                             │
│  • Generated randomly on first run using crypto/rand                        │
│  • Displayed as hex string: "a3f8c2d1e5b94a7c8d2e1f0a3b5c7d9e"              │
│  • Short form for logs: "a3f8c2d1" (first 8 hex chars)                      │
│                                                                             │
│  Identity Storage Options:                                                  │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  FILE-BASED (default):                                                │  │
│  │    • AgentID:    {data_dir}/agent_id                                  │  │
│  │    • PrivateKey: {data_dir}/agent_key       (0600 permissions)        │  │
│  │    • PublicKey:  {data_dir}/agent_key.pub   (0644 permissions)        │  │
│  │                                                                       │  │
│  │  CONFIG-BASED (for single-file deployment):                           │  │
│  │    • agent.id:          32-char hex string                            │  │
│  │    • agent.private_key: 64-char hex string (X25519 private key)       │  │
│  │    • agent.public_key:  optional (derived from private_key)           │  │
│  │                                                                       │  │
│  │  When config-based identity is set:                                   │  │
│  │    • Takes precedence over file-based identity                        │  │
│  │    • data_dir becomes optional                                        │  │
│  │    • Enables true single-file deployment (no external files)          │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 4.1.1 X25519 Keypair for E2E Encryption

Each agent has an X25519 keypair used for end-to-end encryption key exchange:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          E2E ENCRYPTION KEYPAIR                             │
│                                                                             │
│  Type:    X25519 (Curve25519 ECDH)                                          │
│  Size:    32 bytes (256 bits) each                                          │
│                                                                             │
│  Storage precedence:                                                        │
│    1. Config (agent.private_key) - if set, used exclusively                 │
│    2. Files ({data_dir}/agent_key) - default, auto-generated                │
│                                                                             │
│  Config-based identity validation rules:                                    │
│    • private_key alone is valid (public_key derived automatically)          │
│    • public_key without private_key is invalid                              │
│    • If public_key provided, must match derivation from private_key         │
│    • data_dir required only when private_key not in config                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.2 Stream Identification

Streams are identified by a combination of peer connection and stream ID:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          STREAM IDENTIFICATION                              │
│                                                                             │
│  Stream IDs are scoped to a peer connection.                                │
│                                                                             │
│  Allocation scheme (prevents collisions):                                   │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Connection initiator (dialer):   ODD  stream IDs (1, 3, 5, 7...)   │    │
│  │  Connection acceptor (listener):  EVEN stream IDs (2, 4, 6, 8...)   │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Each side maintains its own counter, incrementing by 2.                    │
│  StreamID 0 is reserved for the control channel.                            │
│                                                                             │
│  Global stream reference (for logging/debugging):                           │
│    {PeerID}:{StreamID}                                                      │
│    Example: "a3f8c2d1:42"                                                   │
│                                                                             │
│  Because the ID space is per connection, every peer that dials this agent   │
│  allocates 1, 3, 5..., so the numeric ID alone is NOT a unique identity.    │
│  Every table that tracks live streams -- the transit relay table and the    │
│  terminal handlers alike (exit, forward, UDP, ICMP, file transfer, shell,   │
│  and the initiator-side stream manager) -- MUST key on (PeerID, StreamID),  │
│  and MUST match the frame's source peer before acting on it.                │
│                                                                             │
│  Keying on the ID alone lets a second peer's open displace the first's      │
│  entry, orphaning a live connection whose read loop is still running, and   │
│  lets any peer half-close, close, or reset another peer's stream by         │
│  quoting a small, entirely guessable ID. STREAM_DATA payloads would still   │
│  fail AEAD, but close, reset, and a bare FIN_WRITE carry no payload and     │
│  would be applied unchecked.                                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 4.3 Address Formats

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            ADDRESS FORMATS                                  │
│                                                                             │
│  Transport addresses (configuration):                                       │
│                                                                             │
│    QUIC:       quic://192.168.1.50:4433                                     │
│                quic://[2001:db8::1]:4433                                    │
│                                                                             │
│    HTTP/2:     h2://192.168.1.50:8443/mesh                                  │
│                h2://relay.example.com:443/mesh                              │
│                                                                             │
│    WebSocket:  ws://192.168.1.50:8080/mesh                                  │
│                wss://relay.example.com:443/mesh                             │
│                                                                             │
│  Destination addresses (STREAM_OPEN):                                       │
│                                                                             │
│    Type 0x01:  IPv4      4 bytes     (e.g., 10.5.3.100)                     │
│    Type 0x04:  IPv6      16 bytes    (e.g., 2001:db8::1)                    │
│    Type 0x03:  Domain    1+N bytes   (length-prefixed string)               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 5. Transport Layer

### 5.1 Transport Selection Guide

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        TRANSPORT SELECTION GUIDE                            │
│                                                                             │
│                           ┌─────────────┐                                   │
│                           │   Start     │                                   │
│                           └──────┬──────┘                                   │
│                                  │                                          │
│                                  ▼                                          │
│                        ┌─────────────────┐                                  │
│                        │ UDP permitted?  │                                  │
│                        └────────┬────────┘                                  │
│                                 │                                           │
│                    YES          │          NO                               │
│              ┌──────────────────┴──────────────────┐                        │
│              │                                     │                        │
│              ▼                                     ▼                        │
│     ┌─────────────────┐                  ┌─────────────────┐                │
│     │      QUIC       │                  │ Direct TCP to   │                │
│     │                 │                  │ peer possible?  │                │
│     │ • Best latency  │                  └────────┬────────┘                │
│     │ • Native mux    │                           │                         │
│     │ • 0-RTT resume  │              YES          │          NO             │
│     └─────────────────┘         ┌─────────────────┴─────────────────┐       │
│                                 │                                   │       │
│                                 ▼                                   ▼       │
│                        ┌─────────────────┐                ┌─────────────────┐
│                        │     HTTP/2      │                │   WebSocket     │
│                        │                 │                │                 │
│                        │ • Single TCP    │                │ • Proxy compat  │
│                        │ • Port 443 OK   │                │ • HTTP CONNECT  │
│                        │ • Good perf     │                │ • Most flexible │
│                        └─────────────────┘                └─────────────────┘
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.2 Transport Comparison

| Aspect                    | QUIC                 | HTTP/2            | WebSocket            |
| ------------------------- | -------------------- | ----------------- | -------------------- |
| **Underlying**            | UDP                  | TCP               | TCP                  |
| **Multiplexing**          | Native               | Application-layer | Application-layer    |
| **Head-of-line blocking** | None                 | TCP level         | TCP level            |
| **Connection setup**      | 1-RTT (0-RTT resume) | TCP + TLS         | TCP + TLS + Upgrade  |
| **Proxy traversal**       | Poor                 | Moderate          | Excellent            |
| **Firewall friendliness** | Poor                 | Good              | Excellent            |
| **Best for**              | Performance          | Direct TCP        | Restrictive networks |

### 5.3 Transport Interface

All transports implement a unified interface:

```go
// Transport creates and accepts peer connections
type Transport interface {
    // Dial connects to a remote peer
    Dial(ctx context.Context, addr string, opts DialOptions) (PeerConn, error)

    // Listen creates a listener for incoming connections
    Listen(addr string, opts ListenOptions) (Listener, error)

    // Type returns the transport type identifier
    Type() TransportType

    // Close closes the transport
    Close() error
}

// Listener accepts incoming peer connections
type Listener interface {
    // Accept waits for and returns the next connection
    Accept(ctx context.Context) (PeerConn, error)

    // Addr returns the listener's network address
    Addr() net.Addr

    // Close stops the listener
    Close() error
}

// PeerConn represents a connection to a peer
type PeerConn interface {
    // OpenStream creates a new outgoing stream
    OpenStream(ctx context.Context) (Stream, error)

    // AcceptStream waits for an incoming stream
    AcceptStream(ctx context.Context) (Stream, error)

    // Close terminates the connection
    Close() error

    // LocalAddr returns the local address
    LocalAddr() net.Addr

    // RemoteAddr returns the remote address
    RemoteAddr() net.Addr

    // IsDialer returns true if this side initiated the connection
    IsDialer() bool

    // TransportType returns the transport protocol type
    TransportType() TransportType
}

// Stream is a bidirectional byte stream
type Stream interface {
    io.Reader
    io.Writer

    // StreamID returns the stream identifier
    StreamID() uint64

    // CloseWrite sends a half-close (FIN)
    CloseWrite() error

    // Close fully closes the stream
    Close() error

    // SetDeadline sets read and write deadlines
    SetDeadline(t time.Time) error
    SetReadDeadline(t time.Time) error
    SetWriteDeadline(t time.Time) error
}
```

### 5.4 QUIC Transport

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            QUIC TRANSPORT                                   │
│                                                                             │
│  ┌───────────────┐            UDP            ┌───────────────┐              │
│  │    Agent A    │◄─────────────────────────►│    Agent B    │              │
│  │               │      QUIC Connection      │               │              │
│  │  ┌─────────┐  │                           │  ┌─────────┐  │              │
│  │  │Stream 0 │◄─┼───────────────────────────┼─►│Stream 0 │  │  Control     │
│  │  ├─────────┤  │                           │  ├─────────┤  │              │
│  │  │Stream 1 │◄─┼───────────────────────────┼─►│Stream 1 │  │  Data        │
│  │  ├─────────┤  │                           │  ├─────────┤  │              │
│  │  │Stream 3 │◄─┼───────────────────────────┼─►│Stream 3 │  │  Data        │
│  │  ├─────────┤  │                           │  ├─────────┤  │              │
│  │  │   ...   │  │                           │  │   ...   │  │              │
│  │  └─────────┘  │                           │  └─────────┘  │              │
│  └───────────────┘                           └───────────────┘              │
│                                                                             │
│  Implementation: github.com/quic-go/quic-go                                 │
│                                                                             │
│  Characteristics:                                                           │
│  • Each virtual stream maps to native QUIC stream                           │
│  • No head-of-line blocking between streams                                 │
│  • Built-in TLS 1.3 encryption                                              │
│  • Connection migration (survives IP changes)                               │
│  • 0-RTT session resumption                                                 │
│                                                                             │
│  Configuration:                                                             │
│  • Max streams: 10,000                                                      │
│  • Idle timeout: 90s (matches connections.timeout default)                  │
│  • Keepalive: 30s                                                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.5 HTTP/2 Streaming Transport

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        HTTP/2 STREAMING TRANSPORT                           │
│                                                                             │
│  ┌───────────────┐          TCP+TLS          ┌───────────────┐              │
│  │    Agent A    │◄─────────────────────────►│    Agent B    │              │
│  │   (client)    │       HTTP/2 Conn         │   (server)    │              │
│  │               │                           │               │              │
│  │               │     POST /mesh            │               │              │
│  │  ┌─────────┐  │     ─────────────►        │  ┌─────────┐  │              │
│  │  │ Request │  │     Streaming body        │  │ Handler │  │              │
│  │  │  body   │──┼──────────────────────────►│──│  reads  │  │              │
│  │  │ writer  │  │                           │  │ request │  │              │
│  │  └─────────┘  │                           │  └─────────┘  │              │
│  │               │     ◄─────────────        │               │              │
│  │  ┌─────────┐  │     Streaming resp        │  ┌─────────┐  │              │
│  │  │Response │◄─┼───────────────────────────┼──│Response │  │              │
│  │  │  body   │  │                           │  │ writer  │  │              │
│  │  │ reader  │  │                           │  └─────────┘  │              │
│  │  └─────────┘  │                           │               │              │
│  └───────────────┘                           └───────────────┘              │
│                                                                             │
│  Implementation: golang.org/x/net/http2                                     │
│                                                                             │
│  Characteristics:                                                           │
│  • Single HTTP/2 POST request with streaming body                           │
│  • Response body also streams (bidirectional once established)              │
│  • Our frame protocol provides multiplexing                                 │
│  • Works on port 443, traverses most firewalls                              │
│                                                                             │
│  Key implementation details:                                                │
│  • Use io.Pipe for request body streaming                                   │
│  • Call http.Flusher.Flush() to disable buffering                           │
│  • Set Content-Type: application/octet-stream                               │
│  • Write deadlines poison the pipe's read end (CloseWithError), unblocking  │
│    a Write stalled behind a non-reading peer with os.ErrDeadlineExceeded;   │
│    read deadlines are unsupported (the response body has no timeout knob)   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.6 WebSocket HTTP/1.1 Transport

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      WEBSOCKET HTTP/1.1 TRANSPORT                           │
│                                                                             │
│  ┌───────────────┐    ┌───────────┐    ┌───────────────┐                    │
│  │    Agent A    │────│   HTTP    │────│    Agent B    │                    │
│  │   (client)    │    │   Proxy   │    │   (server)    │                    │
│  └───────┬───────┘    └─────┬─────┘    └───────┬───────┘                    │
│          │                  │                  │                            │
│          │ CONNECT host:443 │                  │                            │
│          │─────────────────►│                  │                            │
│          │                  │ TCP connect      │                            │
│          │                  │─────────────────►│                            │
│          │◄─────────────────│                  │                            │
│          │ 200 Tunnel OK    │                  │                            │
│          │                                     │                            │
│          │◄════════════ TLS Handshake ════════►│                            │
│          │                                     │                            │
│          │ GET /mesh HTTP/1.1                  │                            │
│          │ Upgrade: websocket                  │                            │
│          │────────────────────────────────────►│                            │
│          │                                     │                            │
│          │◄────────────────────────────────────│                            │
│          │ 101 Switching Protocols             │                            │
│          │                                     │                            │
│          │◄═══════ WebSocket Frames ══════════►│                            │
│          │         (bidirectional)             │                            │
│                                                                             │
│  Implementation: nhooyr.io/websocket                                        │
│                                                                             │
│  Characteristics:                                                           │
│  • Maximum compatibility with corporate proxies                             │
│  • HTTP CONNECT tunneling for proxy traversal                               │
│  • After upgrade, fully symmetric bidirectional                             │
│  • Binary frames for efficiency                                             │
│  • Our frame protocol provides multiplexing                                 │
│  • Read/write deadlines are stored on the stream and applied as the         │
│    context deadline of each Read/Write (the library maps them onto the      │
│    underlying net.Conn)                                                     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 5.7 Multiplexing Strategy

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         MULTIPLEXING STRATEGY                               │
│                                                                             │
│  QUIC:                                                                      │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Each virtual stream = dedicated QUIC stream                        │    │
│  │  No additional framing needed for multiplexing                      │    │
│  │  Frames still used for control messages and stream metadata         │    │
│  │  Benefit: Native per-stream flow control, no HOL blocking           │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  HTTP/2 and WebSocket:                                                      │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Single transport stream carries all virtual streams                │    │
│  │  Our frame protocol provides multiplexing via StreamID              │    │
│  │  Writer uses round-robin for fairness between streams               │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Fairness (HTTP/2 and WebSocket):                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Problem: Video stream could starve SSH stream                      │    │
│  │                                                                     │    │
│  │  Solution:                                                          │    │
│  │  • Maximum frame payload: 16 KB                                     │    │
│  │  • Writer maintains queue per stream                                │    │
│  │  • Round-robin between streams with pending data                    │    │
│  │  • No stream can send more than one frame before others get a turn  │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 6. Frame Protocol

### 6.1 Frame Format

All communication uses a consistent framing protocol:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             FRAME FORMAT                                    │
│                                                                             │
│   Offset:  0       1       2                   6                  14        │
│           ┌───────┬───────┬───────────────────┬──────────────────┐          │
│           │ Type  │ Flags │      Length       │     StreamID     │          │
│           │  1B   │  1B   │        4B         │        8B        │          │
│           ├───────┴───────┴───────────────────┴──────────────────┤          │
│           │                                                      │          │
│           │                      Payload                         │          │
│           │                  (Length bytes)                      │          │
│           │                                                      │          │
│           └──────────────────────────────────────────────────────┘          │
│                                                                             │
│   Header size: 14 bytes                                                     │
│   Maximum payload: 16,384 bytes (16 KB)                                     │
│   Maximum frame size: 16,398 bytes                                          │
│                                                                             │
│   Byte order: Big-endian (network byte order)                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 Frame Types

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             FRAME TYPES                                     │
│                                                                             │
│  Stream Frames:                                                             │
│  ┌──────┬────────────────────┬─────────────┬─────────────────────────────┐  │
│  │ Type │ Name               │ Direction   │ Purpose                     │  │
│  ├──────┼────────────────────┼─────────────┼─────────────────────────────┤  │
│  │ 0x01 │ STREAM_OPEN        │ Forward     │ Request to open stream      │  │
│  │ 0x02 │ STREAM_OPEN_ACK    │ Backward    │ Stream opened successfully  │  │
│  │ 0x03 │ STREAM_OPEN_ERR    │ Backward    │ Stream open failed          │  │
│  │ 0x04 │ STREAM_DATA        │ Both        │ Payload data                │  │
│  │ 0x05 │ STREAM_CLOSE       │ Both        │ Graceful close (half/full)  │  │
│  │ 0x06 │ STREAM_RESET       │ Both        │ Abort stream with error     │  │
│  └──────┴────────────────────┴─────────────┴─────────────────────────────┘  │
│                                                                             │
│  Routing Frames:                                                            │
│  ┌──────┬────────────────────┬─────────────┬─────────────────────────────┐  │
│  │ Type │ Name               │ Direction   │ Purpose                     │  │
│  ├──────┼────────────────────┼─────────────┼─────────────────────────────┤  │
│  │ 0x10 │ ROUTE_ADVERTISE    │ Flood       │ Announce CIDR/domain/forward│  │
│  │ 0x11 │ ROUTE_WITHDRAW     │ Flood       │ Remove CIDR/domain/forward  │  │
│  │ 0x12 │ NODE_INFO_ADVERTISE│ Flood       │ Announce node metadata      │  │
│  └──────┴────────────────────┴─────────────┴─────────────────────────────┘  │
│                                                                             │
│  Route Address Families:                                                    │
│  ┌──────┬──────────────────┬────────────────────────────────────────────┐   │
│  │ Type │ Name             │ Description                                │   │
│  ├──────┼──────────────────┼────────────────────────────────────────────┤   │
│  │ 0x01 │ AddrFamilyIPv4   │ IPv4 CIDR route (e.g., 10.0.0.0/8)         │   │
│  │ 0x02 │ AddrFamilyIPv6   │ IPv6 CIDR route (e.g., ::/0)               │   │
│  │ 0x03 │ AddrFamilyDomain │ Domain pattern (exact or wildcard)         │   │
│  │ 0x04 │ AddrFamilyForward│ Port forward routing key + target          │   │
│  │ 0x05 │ AddrFamilyAgent  │ Agent presence (128-bit agent ID)          │   │
│  └──────┴──────────────────┴────────────────────────────────────────────┘   │
│                                                                             │
│  Agent Presence Routes:                                                     │
│  Every agent unconditionally advertises a presence route (AddrFamilyAgent)  │
│  containing its 128-bit agent ID. This makes all agents reachable by ID     │
│  for control requests, shell, file transfer, and ICMP -- even without       │
│  exit routes configured. Presence routes use metric 0 and are advertised    │
│  alongside any CIDR/domain/forward routes.                                  │
│                                                                             │
│  Domain Route Wire Format in ROUTE_ADVERTISE:                               │
│  • AddressFamily: 0x03 (domain)                                             │
│  • PrefixLength: 0x00 (exact) or 0x01 (wildcard)                            │
│  • Prefix: 1-byte length + UTF-8 domain pattern                             │
│  • Metric: uint16                                                           │
│                                                                             │
│  Forward Route Wire Format (AddrFamilyForward):                             │
│  • AddressFamily: 0x04                                                      │
│  • PrefixLength: 0x00 (unused)                                              │
│  • Prefix: [KeyLen(1)][Key][TargetLen(1)][Target]                           │
│  • Metric: uint16                                                           │
│                                                                             │
│  ROUTE_WITHDRAW envelope structure:                                         │
│  • OriginAgent (16), Sequence (8)                                           │
│  • RouteCount (1), Routes[] (same per-route encoding as ROUTE_ADVERTISE,    │
│    so domain and forward withdrawals carry their variable-length prefix)    │
│  • SeenByCount (1), SeenBy[] (16 bytes each)                                │
│  Withdrawal is per-route: removing a single dynamic CIDR, domain, forward,  │
│  or expose-attached route floods exactly that prefix so peers prune their   │
│  tables immediately rather than waiting for the route_ttl to expire.        │
│                                                                             │
│  ROUTE_ADVERTISE envelope structure:                                        │
│  • OriginAgent (16), OriginDisplayName (1+N), Sequence (8)                  │
│  • RouteCount (1), Routes[]                                                 │
│    RouteCount is a single byte, so at most 255 routes ride in one message.  │
│    An origin with more splits across several advertisements, and each split │
│    carries its OWN sequence; the same applies to ROUTE_WITHDRAW. Receivers  │
│    dedupe on (OriginAgent, Sequence), so splits sharing one sequence would  │
│    be discarded as duplicates and only the first would propagate. Emitting  │
│    a wrapped count instead yields a frame the peer cannot decode, and since │
│    a decode error drops the connection and the next attempt re-sends it,    │
│    the link never recovers. Length-prefixed strings (display name,          │
│    comments) and 1-byte element counts are capped for the same reason.      │
│                                                                             │
│    When re-advertising ANOTHER agent's routes (full-table push on peer      │
│    connect), routes are grouped by the sequence they were learned under and │
│    one advertisement is sent per group. A locally minted sequence must      │
│    never be used there -- see "Sequence ownership on re-advertisement".     │
│  • EncryptedData{Encrypted(1) + DataLen(2) + Data} wrapping the Path list   │
│    (plaintext or management-key-encrypted; see NODE_INFO_ADVERTISE)         │
│  • SeenByCount (1), SeenBy[] (16 bytes each)                                │
│  • Optional trailing block: CommentCount (1), Comments[] (each 1+N bytes,   │
│    parallel to Routes). Omitted entirely when no route carries a comment.   │
│    Older agents (pre-0834122) ignore the trailing block; new receivers      │
│    decode it when present and store the comment on the peer-learned route.  │
│                                                                             │
│  Control Frames:                                                            │
│  ┌──────┬────────────────────┬─────────────┬─────────────────────────────┐  │
│  │ Type │ Name               │ Direction   │ Purpose                     │  │
│  ├──────┼────────────────────┼─────────────┼─────────────────────────────┤  │
│  │ 0x20 │ PEER_HELLO         │ Initiator   │ Initial handshake           │  │
│  │ 0x21 │ PEER_HELLO_ACK     │ Acceptor    │ Handshake response          │  │
│  │ 0x22 │ KEEPALIVE          │ Either      │ Liveness probe              │  │
│  │ 0x23 │ KEEPALIVE_ACK      │ Either      │ Liveness response           │  │
│  │ 0x24 │ CONTROL_REQUEST    │ Either      │ Request status/RPC          │  │
│  │ 0x25 │ CONTROL_RESPONSE   │ Either      │ Response with data          │  │
│  └──────┴────────────────────┴─────────────┴─────────────────────────────┘  │
│                                                                             │
│  Control Request Types (in CONTROL_REQUEST payload):                        │
│  ┌──────┬────────────────────┬──────────────────────────────────────────┐   │
│  │ Type │ Name               │ Purpose                                  │   │
│  ├──────┼────────────────────┼──────────────────────────────────────────┤   │
│  │ 0x02 │ STATUS             │ Request agent status                     │   │
│  │ 0x03 │ PEERS              │ Request peer list                        │   │
│  │ 0x04 │ ROUTES             │ Request route table                      │   │
│  │ 0x05 │ RPC                │ Remote procedure call (shell command)    │   │
│  │ 0x08 │ ROUTE_MANAGE       │ Add, remove, or list dynamic routes      │   │
│  │ 0x09 │ FORWARD_MANAGE     │ Add, remove, or list forward listeners   │   │
│  │ 0x0A │ FILE_BROWSE        │ File browsing                            │   │
   │      │                    │ (list, stat, roots, chmod, delete)       │   │
│  │ 0x0B │ DISPLAY_NAME_MANAGE│ Dynamic display name management          │   │
│  │ 0x0C │ PEER_MANAGE        │ Add, remove, or list dynamic peers       │   │
│  │ 0x0D │ PROBE              │ Connectivity probe (dial + handshake)    │   │
│  │ 0x0E │ FORWARD_ENDPOINT_  │ Add, remove, or list forward endpoints   │   │
│  │      │ MANAGE             │                                          │   │
│  │ 0x0F │ ROUTE_TRACE        │ Route selection trace (diagnostic)       │   │
│  │ 0x10 │ STOP               │ Graceful agent shutdown (signed)         │   │
│  │ 0x11 │ LISTENER_MANAGE    │ Add, remove, or list transport listeners │   │
│  └──────┴────────────────────┴──────────────────────────────────────────┘   │
│                                                                             │
│  UDP Frames (for SOCKS5 UDP ASSOCIATE):                                     │
│  ┌──────┬────────────────────┬─────────────┬─────────────────────────────┐  │
│  │ Type │ Name               │ Direction   │ Purpose                     │  │
│  ├──────┼────────────────────┼─────────────┼─────────────────────────────┤  │
│  │ 0x30 │ UDP_OPEN           │ Forward     │ Request UDP association     │  │
│  │ 0x31 │ UDP_OPEN_ACK       │ Backward    │ Association established     │  │
│  │ 0x32 │ UDP_OPEN_ERR       │ Backward    │ Association failed          │  │
│  │ 0x33 │ UDP_DATAGRAM       │ Both        │ UDP datagram payload        │  │
│  │ 0x34 │ UDP_CLOSE          │ Both        │ Close association           │  │
│  └──────┴────────────────────┴─────────────┴─────────────────────────────┘  │
│                                                                             │
│  File Transfer: Uses special domain addresses in STREAM_OPEN:               │
│  • "file:upload" - Upload file to remote agent                              │
│  • "file:download" - Download file from remote agent                        │
│                                                                             │
│  ICMP Frames (for ping through mesh):                                       │
│  ┌──────┬────────────────────┬─────────────┬─────────────────────────────┐  │
│  │ Type │ Name               │ Direction   │ Purpose                     │  │
│  ├──────┼────────────────────┼─────────────┼─────────────────────────────┤  │
│  │ 0x40 │ ICMP_OPEN          │ Forward     │ Request ICMP echo session   │  │
│  │ 0x41 │ ICMP_OPEN_ACK      │ Backward    │ Session established         │  │
│  │ 0x42 │ ICMP_OPEN_ERR      │ Backward    │ Session failed              │  │
│  │ 0x43 │ ICMP_ECHO          │ Both        │ Echo request/reply payload  │  │
│  │ 0x44 │ ICMP_CLOSE         │ Both        │ Close ICMP session          │  │
│  └──────┴────────────────────┴─────────────┴─────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.3 Frame Flags

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              FRAME FLAGS                                    │
│                                                                             │
│   Bit   │ Name          │ Applicable To          │ Meaning                  │
│  ───────┼───────────────┼────────────────────────┼───────────────────────── │
│    0    │ FIN_WRITE     │ STREAM_DATA/CLOSE      │ Sender is done writing   │
│    1    │ FIN_READ      │ STREAM_CLOSE           │ Reserved; treat as hard  │
│         │               │                        │ close (see below)        │
│    2    │ (reserved)    │                        │                          │
│    3    │ (reserved)    │                        │                          │
│   4-7   │ (reserved)    │                        │                          │
│                                                                             │
│   FIN_WRITE can be set on STREAM_DATA to signal half-close with final data  │
│   or on STREAM_CLOSE for half-close without additional data.                │
│                                                                             │
│   Valid ways to signal half-close:                                          │
│   Method A: Piggyback FIN_WRITE on the final STREAM_DATA frame              │
│            (atomic: delivers data + half-close together)                    │
│   Method B: Send STREAM_CLOSE with flags = FIN_WRITE                        │
│            (when no final data, or after data has already been sent)        │
│   Method C: Send STREAM_CLOSE with flags = FIN_WRITE|FIN_READ               │
│            (full close)                                                     │
│                                                                             │
│   FIN_READ note: TCP has no "done receiving" signal and Muti Metroo does not      │
│   use FIN_READ in practice. A FIN_READ-only STREAM_CLOSE is equivalent to   │
│   STREAM_RESET; receivers MUST tolerate it as a hard close. Prefer          │
│   STREAM_RESET when aborting.                                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 6.4 Payload Definitions

#### PEER_HELLO (0x20)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              PEER_HELLO                                     │
│                                                                             │
│   Sent by connection initiator after transport is established.              │
│                                                                             │
│   ┌─────────────────┬────────┬──────────────────────────────────────────┐   │
│   │ Field           │ Size   │ Description                              │   │
│   ├─────────────────┼────────┼──────────────────────────────────────────┤   │
│   │ Version         │ 2      │ Protocol version (currently 1)           │   │
│   │ AgentID         │ 16     │ Sender's agent ID                        │   │
│   │ Timestamp       │ 8      │ Unix timestamp (seconds)                 │   │
│   │ DisplayName     │ 1+N    │ Length-prefixed UTF-8 display name       │   │
│   │ CapabilitiesLen │ 1      │ Number of capabilities                   │   │
│   │ Capabilities    │ varies │ List of capability strings               │   │
│   └─────────────────┴────────┴──────────────────────────────────────────┘   │
│                                                                             │
│   Capability string format: 1-byte length + UTF-8 string                    │
│   The capabilities list is reserved for future use; the current agent       │
│   implementation advertises an empty list. Node roles are discovered        │
│   through NODE_INFO_ADVERTISE instead.                                      │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### STREAM_OPEN (0x01)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             STREAM_OPEN                                     │
│                                                                             │
│   Request to open a virtual stream to a destination.                        │
│                                                                             │
│   ┌─────────────────┬────────┬──────────────────────────────────────────┐   │
│   │ Field           │ Size   │ Description                              │   │
│   ├─────────────────┼────────┼──────────────────────────────────────────┤   │
│   │ RequestID       │ 8      │ Unique ID for correlating ACK/ERR        │   │
│   │ AddressType     │ 1      │ 0x01=IPv4, 0x04=IPv6, 0x03=Domain        │   │
│   │ Address         │ varies │ Address bytes (see below)                │   │
│   │ Port            │ 2      │ Destination port                         │   │
│   │ TTL             │ 1      │ Remaining hops (decremented each hop)    │   │
│   │ PathLength      │ 1      │ Number of agents in remaining path       │   │
│   │ RemainingPath   │ varies │ Array of AgentIDs (16 bytes each)        │   │
│   │ EphemeralPubKey │ 32     │ X25519 public key for E2E encryption     │   │
│   └─────────────────┴────────┴──────────────────────────────────────────┘   │
│                                                                             │
│   Address encoding:                                                         │
│   • IPv4 (0x01):   4 bytes, network order                                   │
│   • IPv6 (0x04):   16 bytes, network order                                  │
│   • Domain (0x03): 1-byte length + UTF-8 domain name                        │
│                                                                             │
│   The ephemeral public key is used to establish E2E encryption between      │
│   ingress and exit agents. Transit agents forward this key unchanged.       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### STREAM_OPEN_ACK (0x02)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           STREAM_OPEN_ACK                                   │
│                                                                             │
│   Sent by exit agent to acknowledge successful stream open.                 │
│                                                                             │
│   ┌─────────────────┬────────┬──────────────────────────────────────────┐   │
│   │ Field           │ Size   │ Description                              │   │
│   ├─────────────────┼────────┼──────────────────────────────────────────┤   │
│   │ RequestID       │ 8      │ Correlates with STREAM_OPEN request      │   │
│   │ BoundAddrType   │ 1      │ 0x01=IPv4, 0x04=IPv6                     │   │
│   │ BoundAddress    │ 4 or 16│ Bound local address                      │   │
│   │ BoundPort       │ 2      │ Bound local port                         │   │
│   │ EphemeralPubKey │ 32     │ Exit's X25519 public key for E2E         │   │
│   └─────────────────┴────────┴──────────────────────────────────────────┘   │
│                                                                             │
│   The ephemeral public key allows the ingress agent to compute the same     │
│   shared secret via X25519 ECDH for end-to-end encryption.                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### STREAM_DATA (0x04)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                             STREAM_DATA                                     │
│                                                                             │
│   Encrypted stream data payload. Max payload: 16 KB.                        │
│                                                                             │
│   Payload is encrypted with ChaCha20-Poly1305:                              │
│   ┌─────────────────┬────────┬──────────────────────────────────────────┐   │
│   │ Field           │ Size   │ Description                              │   │
│   ├─────────────────┼────────┼──────────────────────────────────────────┤   │
│   │ Direction       │ 4      │ 0x80000000 if responder->initiator, else │   │
│   │                 │        │ 0x00000000 (only the high bit is used)   │   │
│   │ Counter         │ 8      │ Big-endian frame counter starting at 0   │   │
│   │ Ciphertext      │ varies │ Encrypted application data               │   │
│   │ AuthTag         │ 16     │ Poly1305 authentication tag              │   │
│   └─────────────────┴────────┴──────────────────────────────────────────┘   │
│                                                                             │
│   The 12-byte nonce fed to ChaCha20-Poly1305 is the concatenation           │
│   Direction || Counter.                                                     │
│                                                                             │
│   On receive, the Direction field MUST be compared against the direction    │
│   the receiver expects to receive from, before the counter is checked.      │
│   One symmetric key serves both directions, so a frame replayed back at     │
│   its own sender authenticates unless Direction is verified. Checking it    │
│   before the counter also stops a rejected frame from advancing the         │
│   anti-replay high-water mark and starving legitimate traffic.              │
│                                                                             │
│   Encryption overhead: 28 bytes per frame (12 nonce + 16 tag)               │
│                                                                             │
│   Flags:                                                                    │
│   • FIN_WRITE (0x01): Sender half-close (no more writes)                    │
│   • FIN_READ (0x02): Receiver half-close (no more reads)                    │
│                                                                             │
│   Transit agents forward encrypted payloads unchanged (cannot decrypt).     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### 6.4.1 Session Key Derivation

All E2E-encrypted sessions (TCP streams, port-forward streams, UDP associations, and
ICMP echo sessions) derive their session key using the same function:

```go
DeriveSessionKey(
    sharedSecret       [32]byte,   // X25519 ECDH output
    streamOrRequestID  uint64,     // StreamID for TCP/forward, RequestID for UDP/ICMP
    initiatorPub       [32]byte,   // Initiator (ingress) ephemeral public key
    responderPub       [32]byte,   // Responder (exit) ephemeral public key
    isInitiator        bool,       // true on the ingress side, false on the exit side
) *SessionKey
```

Key derivation uses HKDF-SHA256:

- **IKM:** 32-byte X25519 shared secret (ECDH of local private and remote public)
- **Salt:** `streamOrRequestID (8 BE) || initiatorPub (32) || responderPub (32)` = 72 bytes
- **Info:** ASCII string `"muti-metroo-e2e-v1"`
- **L (output length):** 32 bytes (ChaCha20-Poly1305 key size)

Direction in the resulting nonce is determined by `isInitiator` (see the STREAM_DATA
Direction field above). The initiator encrypts with Direction = `0x00000000` and
decrypts with Direction = `0x80000000`; the responder uses the opposite convention.

Source: `internal/crypto/crypto.go`.

#### 6.4.2 Protocol Evolution Policy

- `NODE_INFO_ADVERTISE` fields after `PublicKey` are decoded only if bytes remain in
  the buffer. New optional fields MUST be appended at the end and MUST be
  self-delimiting (typically length-prefixed). Existing ordering is normative;
  reordering requires a protocol version bump.
- The `capabilities` list in `PEER_HELLO` is reserved for future negotiation. Until
  negotiation is defined, senders SHOULD emit `CapabilitiesLen = 0` and receivers
  MUST ignore any capability strings encountered.
- The wire protocol version in `PEER_HELLO` is currently `0x01`. Breaking frame-level
  changes require a version bump; backward-compatible additions should use the
  trailing-optional pattern described above.

#### STREAM_OPEN_ERR (0x03)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          STREAM_OPEN_ERR                                    │
│                                                                             │
│   Sent when a stream open fails anywhere along the path.                    │
│                                                                             │
│   ┌─────────────────┬────────┬──────────────────────────────────────────┐   │
│   │ Field           │ Size   │ Description                              │   │
│   ├─────────────────┼────────┼──────────────────────────────────────────┤   │
│   │ RequestID       │ 8      │ Correlates with STREAM_OPEN request      │   │
│   │ ErrorCode       │ 2      │ Numeric error code (see table below)     │   │
│   │ MessageLen      │ 1      │ Length of human-readable message         │   │
│   │ Message         │ 0..255 │ UTF-8 message truncated to 255 bytes     │   │
│   └─────────────────┴────────┴──────────────────────────────────────────┘   │
│                                                                             │
│   STREAM_RESET carries only a 2-byte ErrorCode (no RequestID or message).   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Error Codes (shared by STREAM_OPEN_ERR, STREAM_RESET, UDP/ICMP errors)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           ERROR CODES                                       │
│                                                                             │
│   ┌───────┬──────────────────────┬────────────────────────────────────┐     │
│   │ Code  │ Name                 │ Meaning                            │     │
│   ├───────┼──────────────────────┼────────────────────────────────────┤     │
│   │ 1     │ NO_ROUTE             │ No route to destination            │     │
│   │ 2     │ CONNECTION_REFUSED   │ Target refused connection          │     │
│   │ 3     │ CONNECTION_TIMEOUT   │ Connection attempt timed out       │     │
│   │ 4     │ TTL_EXCEEDED         │ TTL reached zero                   │     │
│   │ 5     │ HOST_UNREACHABLE     │ Cannot reach target host           │     │
│   │ 6     │ NETWORK_UNREACHABLE  │ Cannot reach target network        │     │
│   │ 7     │ DNS_ERROR            │ Domain name resolution failed      │     │
│   │ 8     │ EXIT_DISABLED        │ Exit functionality not enabled     │     │
│   │ 9     │ RESOURCE_LIMIT       │ Too many streams                   │     │
│   │ 10    │ CONNECTION_LIMIT     │ Connection limit exceeded          │     │
│   │ 11    │ NOT_ALLOWED          │ Operation not permitted            │     │
│   │ 12    │ FILE_TRANSFER_DENIED │ File transfer not allowed          │     │
│   │ 13    │ AUTH_REQUIRED        │ Authentication required            │     │
│   │ 14    │ PATH_NOT_ALLOWED     │ Path not in allowed list           │     │
│   │ 15    │ FILE_TOO_LARGE       │ File exceeds size limit            │     │
│   │ 16    │ FILE_NOT_FOUND       │ File does not exist                │     │
│   │ 17    │ WRITE_FAILED         │ Write operation failed             │     │
│   │ 18    │ GENERAL_FAILURE      │ General error (e.g., key exchange) │     │
│   │ 19    │ RESUME_FAILED        │ Partial transfer cannot resume     │     │
│   │ 20    │ SHELL_DISABLED       │ Shell feature is disabled          │     │
│   │ 21    │ SHELL_AUTH_FAILED    │ Shell authentication failed        │     │
│   │ 22    │ PTY_FAILED           │ PTY allocation failed              │     │
│   │ 23    │ COMMAND_NOT_ALLOWED  │ Command not in shell whitelist     │     │
│   │ 30    │ UDP_DISABLED         │ UDP relay is disabled              │     │
│   │ 31    │ UDP_PORT_NOT_ALLOWED │ UDP port not in whitelist          │     │
│   │ 40    │ FORWARD_NOT_FOUND    │ Port forward key not configured    │     │
│   │ 50    │ ICMP_DISABLED        │ ICMP feature is disabled           │     │
│   │ 51    │ ICMP_DEST_NOT_ALLOWED│ Destination not in allowed CIDRs   │     │
│   │ 52    │ ICMP_SESSION_LIMIT   │ Max concurrent sessions reached    │     │
│   └───────┴──────────────────────┴────────────────────────────────────┘     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### NODE_INFO_ADVERTISE (0x12)

Flooded through the mesh to announce node metadata. The NodeInfo payload may be encrypted
with the management key for topology compartmentalization.

**Envelope (NodeInfoAdvertise):**

```
+------------------+--------+--------------------------------------------------+
| Field            | Size   | Description                                      |
+------------------+--------+--------------------------------------------------+
| OriginAgent      | 16     | Agent advertising its info (128-bit AgentID)     |
| Sequence         | 8      | Monotonically increasing sequence (uint64)       |
| Encrypted        | 1      | 0x00 = plaintext, 0x01 = encrypted               |
| DataLen          | 2      | Length of NodeInfo data (uint16)                 |
| Data             | varies | NodeInfo bytes (plaintext or encrypted blob)     |
| SeenByCount      | 1      | Number of agents in SeenBy list                  |
| SeenBy[]         | N*16   | Agent IDs for loop prevention                    |
+------------------+--------+--------------------------------------------------+
```

When `Encrypted` is 0x01, the `Data` field contains `EphemeralPub(32) + Nonce(24) + Ciphertext + Tag(16)`
and must be decrypted with the management key before decoding as NodeInfo.

**NodeInfo payload (encoding order):**

```
+---------------------------+--------+--------------------------------------------------+
| Field                     | Size   | Description                                      |
+---------------------------+--------+--------------------------------------------------+
| DisplayName               | 1+N    | Length-prefixed UTF-8 string                     |
| Hostname                  | 1+N    | Length-prefixed UTF-8 string                     |
| OS                        | 1+N    | Length-prefixed UTF-8 string                     |
| Arch                      | 1+N    | Length-prefixed UTF-8 string                     |
| Version                   | 1+N    | Length-prefixed UTF-8 string                     |
| StartTime                 | 8      | Unix timestamp (uint64)                          |
| IPCount                   | 1      | Number of IP addresses                           |
| IPAddresses[]             | 1+N ea | Length-prefixed strings (one per IPCount)        |
| PeerCount                 | 1      | Number of connected peers                        |
| Peers[]                   | varies | Per peer: PeerID(16) + Transport(1+N)            |
|                           |        |   + RTTMs(8) + IsDialer(1)                       |
| PublicKey                 | 32     | X25519 public key for E2E encryption             |
| UDPEnabled *              | 1      | 0x00 = disabled, 0x01 = enabled                  |
| ForwardListenerCount *    | 1      | Number of forward listeners                      |
| ForwardListeners[] *      | varies | Per listener: Key(1+N) + Address(1+N)            |
| ShellCount *              | 1      | Number of shell commands                         |
| Shells[] *                | 1+N ea | Length-prefixed strings (whitelisted commands)   |
| FileTransferEnabled *     | 1      | 0x00 = disabled, 0x01 = enabled                  |
| ShellEnabled *            | 1      | 0x00 = disabled, 0x01 = enabled                  |
| IcmpEnabled *             | 1      | 0x00 = disabled, 0x01 = enabled                  |
| AdvertisedListenerCount * | 1      | Number of advertised listeners                   |
| AdvertisedListeners[] *   | varies | Per listener: Transport(1+N) + Address(1+N)      |
|                           |        |   + Path(1+N)                                    |
+---------------------------+--------+--------------------------------------------------+

* Optional fields -- guarded by remaining-bytes check in decoder for backward
  compatibility with older agents. Fields after PublicKey are decoded only if
  bytes remain in the buffer.

Size limits (non-configurable, enforced on encode):
  - Max peers in NodeInfo: 50 (MaxPeersInNodeInfo)
  - Max forward listeners: 20 (MaxForwardListenersInNodeInfo)
  - Max shells: 10 (MaxShellsInNodeInfo)
  - Max advertised listeners: 8 (MaxAdvertisedListenersInNodeInfo)
```

Source: `internal/protocol/frame.go` -- `EncodeNodeInfo()` / `DecodeNodeInfo()`

---

## 6.5 UDP Relay Protocol

SOCKS5 UDP ASSOCIATE (RFC 1928) enables tunneling UDP traffic through the mesh network.

### UDP Association Lifecycle

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                     UDP ASSOCIATION LIFECYCLE                                │
│                                                                              │
│  Client              Ingress              Transit             Exit           │
│    │                    │                    │                  │            │
│    │ UDP ASSOCIATE      │                    │                  │            │
│    │───────────────────>│                    │                  │            │
│    │                    │                    │                  │            │
│    │  Reply (relay addr)│                    │                  │            │
│    │<───────────────────│                    │                  │            │
│    │                    │                    │                  │            │
│    │                    │    UDP_OPEN        │                  │            │
│    │                    │───────────────────>│   UDP_OPEN       │            │
│    │                    │                    │─────────────────>│            │
│    │                    │                    │                  │            │
│    │                    │                    │   UDP_OPEN_ACK   │            │
│    │                    │   UDP_OPEN_ACK     │<─────────────────│            │
│    │                    │<───────────────────│                  │            │
│    │                    │                    │                  │            │
│    │ UDP datagram       │                    │                  │            │
│    │~~~~~~~~~~~~~~~~~~~>│  UDP_DATAGRAM      │  UDP_DATAGRAM    │ UDP packet │
│    │                    │~~~~~~~~~~~~~~~~~~~>│~~~~~~~~~~~~~~~~~>│~~~~~~~~~~~>│
│    │                    │                    │                  │            │
│    │ UDP response       │                    │                  │ UDP reply  │
│    │<~~~~~~~~~~~~~~~~~~~│  UDP_DATAGRAM      │  UDP_DATAGRAM    │<~~~~~~~~~~~│
│    │                    │<~~~~~~~~~~~~~~~~~~~│<~~~~~~~~~~~~~~~~~│            │
│    │                    │                    │                  │            │
│    │ TCP close          │                    │                  │            │
│    │───────────────────>│   UDP_CLOSE        │   UDP_CLOSE      │            │
│    │                    │───────────────────>│─────────────────>│            │
│                                                                              │
│  Legend: ───> = TCP/Control  ~~~> = UDP/Datagram                             │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

### UDP Payload Definitions

#### UDP_OPEN (0x30)

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ RequestID       │ 8      │ Stable correlation ID across hops        │
│ AddressType     │ 1      │ 0x01=IPv4, 0x04=IPv6, 0x03=Domain        │
│ Address         │ varies │ Relay bind address (usually 0.0.0.0)     │
│ Port            │ 2      │ Relay bind port                          │
│ TTL             │ 1      │ Hop limit                                │
│ PathLen         │ 1      │ Number of remaining hops                 │
│ RemainingPath   │ 16*N   │ AgentIDs of remaining path nodes         │
│ EphemeralPubKey │ 32     │ X25519 public key for E2E encryption     │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

UDP_OPEN establishes the relay association (mirroring SOCKS5 UDP ASSOCIATE); its
Address/Port are the relay bind, not a destination. The actual per-datagram
destination travels in each UDP_DATAGRAM frame.

#### UDP_OPEN_ACK (0x31)

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ RequestID       │ 8      │ Correlation ID from UDP_OPEN             │
│ AddressType     │ 1      │ 0x01=IPv4, 0x04=IPv6 (bound relay addr)  │
│ Address         │ varies │ Relay bind address (4 or 16 bytes)       │
│ Port            │ 2      │ Relay bind port                          │
│ EphemeralPubKey │ 32     │ X25519 public key for E2E encryption     │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

#### UDP_OPEN_ERR (0x32)

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ RequestID       │ 8      │ Correlation ID from UDP_OPEN             │
│ ErrorCode       │ 2      │ Error code (see error codes table)       │
│ MessageLen      │ 1      │ Length of error message                  │
│ Message         │ N      │ Human-readable error message             │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

#### UDP_DATAGRAM (0x33)

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ AddressType     │ 1      │ Target address type                      │
│ Address         │ varies │ Target IP or domain                      │
│ Port            │ 2      │ Target port                              │
│ DataLen         │ 2      │ Length of encrypted data                 │
│ Data            │ varies │ Encrypted UDP payload (E2E ChaCha20)     │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

#### UDP_CLOSE (0x34)

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ Reason          │ 1      │ Close reason:                            │
│                 │        │   0 = Normal termination                 │
│                 │        │   1 = Idle timeout                       │
│                 │        │   2 = Error occurred                     │
│                 │        │   3 = TCP control connection closed      │
│                 │        │   4 = Administrative close               │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

### Design Notes

- **Fragmentation**: Not supported. Datagrams with frag > 0 are rejected.
- **Max Datagram Size**: 1472 bytes (MTU - IP/UDP headers)
- **Association Lifetime**: Tied to TCP control connection. When TCP closes, UDP association terminates.
- **Access Control**: Uses CIDR-based exit routes (same as TCP streams).
- **Authentication**: Uses existing SOCKS5 authentication (not separate password).

---

## 6.6 Port Forwarding (Reverse Tunnel)

Port forwarding enables reverse tunneling through the mesh - exposing local services to remote agents. Unlike SOCKS5 (outbound: local client -> mesh -> remote destination), port forwarding routes inbound traffic (remote client -> mesh -> local service).

### Architecture

**Components:**

- **Endpoint** (`internal/forward/handler.go`): Exit point where local services are exposed
- **Listener** (`internal/forward/listener.go`): Ingress point accepting remote connections
- **Routing Key**: String identifier matching listeners to endpoints

**Traffic Flow:**

```
Remote Client
    |
    | TCP connect
    |
    v
Listener Agent (forward.listeners)
    |
    | DialForward("routing-key")
    | Route lookup -> ForwardRoute
    |
    v
Transit Agent(s)
    |
    | Frame relay (E2E encrypted)
    |
    v
Endpoint Agent (forward.endpoints)
    |
    | TCP dial target
    |
    v
Local Service (target: "localhost:80")
```

### Frame Protocol

Port forwarding uses standard `STREAM_OPEN` frames with special address format:

**Address Encoding:**

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ AddressType     │ 1      │ 0x03 (AddrTypeDomain)                    │
│ AddressLength   │ 1      │ Length of address string                 │
│ Address         │ varies │ "forward:<routing-key>" (ASCII)          │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

**Protocol Constant:**

```go
const ForwardStreamPrefix = "forward:"
```

Example: For routing key "tools", the address bytes are: `\x03\x0dforward:tools` (ATYP 0x03, length 0x0d = 13, then `forward:tools`)

### Route Advertisement

Forward routes propagate via flood routing (same mechanism as CIDR routes):

**ForwardRoute Structure:**

```go
type ForwardRoute struct {
    Key         string             // Routing key
    Target      string             // Endpoint target (host:port)
    NextHop     AgentID            // Next hop toward origin
    OriginAgent AgentID            // Agent advertising endpoint
    Metric      uint16             // Origin metric + 1 (same width as Route.Metric; not a hop count)
    Path        []AgentID          // Full path (for loop prevention)
    EncPath     *EncryptedData     // Encrypted path payload (management-key encrypted; see NODE_INFO_ADVERTISE)
    Sequence    uint64             // Advertisement sequence number
    LastUpdate  time.Time          // Expiry computed as now - LastUpdate > ttl
}
```

**Advertisement Process:**

1. Endpoint agent registers local route via `AddLocalForwardRoute(key, target, 0)`
2. Route included in periodic flood advertisements
3. Each receiving agent stores the route with `metric + 1` and re-floods the advertisement with the metric unchanged, so every agent holds the origin metric plus one regardless of distance (the same holds for CIDR, domain, and agent presence routes)
4. Listener agents store routes in forward routing table
5. On connection, listener performs `LookupForward(key)` to find best route

### E2E Encryption

Each forwarded connection establishes independent E2E encryption:

1. Listener generates ephemeral X25519 keypair
2. `STREAM_OPEN` includes listener's public key
3. Endpoint generates ephemeral keypair, derives shared secret via ECDH
4. `STREAM_OPEN_ACK` includes endpoint's public key
5. Both derive session key via `DeriveSessionKey` (see section 6.4.1). The
   listener is the initiator and supplies its StreamID as `streamOrRequestID`.
6. All `STREAM_DATA` encrypted with ChaCha20-Poly1305

Transit agents relay encrypted frames without decryption capability.

### Configuration

**Endpoints (where service runs):**

```yaml
forward:
  endpoints:
    - key: "web-server" # Routing key
      target: "localhost:3000" # Local service
```

**Listeners (where clients connect):**

```yaml
forward:
  listeners:
    - key: "web-server" # Must match endpoint key
      address: ":8080" # Bind address
      max_connections: 100 # Optional limit
```

### Dynamic Management

Endpoints and listeners can both be added, removed, and listed at runtime. The
`forward.Handler.targets` map is guarded by an RWMutex so add/remove races
cannot tear the read path (`GetTarget`, `HandleStreamOpen`). The Agent keeps
per-origin sets (`configForwardEndpoints`, `dynamicForwardEndpoints`, and the
same split for listeners) so config-sourced entries cannot be mutated via the
CLI/HTTP API and the `list` action can flag each entry's origin.

Runtime mutations are persisted to the append-only JSONL journal at
`<data_dir>/dynamic-config.log`. Four op kinds cover the forward surface:
`forward_add` / `forward_remove` for listeners, and `forward_endpoint_add` /
`forward_endpoint_remove` for endpoints. On agent restart the journal is
replayed through the same `ManageForward*` entry points that CLI and HTTP
callers use, then compacted to the minimal live-state snapshot. When
`data_dir` is empty the journal is a no-op and runtime changes stay
ephemeral.

Endpoint mutations trigger `TriggerRouteAdvertise()` so the `ForwardRoute`
reaches peers as quickly as a static endpoint would after initial
advertisement. Listener mutations trigger `TriggerNodeInfoAdvertise()`
because listeners surface in node-info, not the route table.

### Error Codes

| Code | Name                   | Description                                           |
| ---- | ---------------------- | ----------------------------------------------------- |
| 40   | `ErrForwardNotFound`   | Routing key not configured on endpoint                |
| 2    | `ErrConnectionRefused` | Target service refused connection (shared error code) |
| 3    | `ErrConnectionTimeout` | Target dial timed out (shared error code)             |

### Package Structure

```
internal/forward/
├── forward.go        # Endpoint struct, ForwardDialer interface
├── handler.go        # Exit point handler (processes STREAM_OPEN for forward)
├── listener.go       # TCP listener (accepts connections, calls DialForward)
├── forward_test.go   # Forward unit tests
├── handler_test.go   # Handler unit tests
└── listener_test.go  # Listener unit tests
```

### Ephemeral Expose Endpoints over SOCKS5 (`internal/expose`)

In addition to config-defined and journalled dynamic forward endpoints, the
agent supports ephemeral endpoints whose lifetime is bound to an expose
session. An authenticated client (typically Mutiauk running on a
firewalled host) opens the SOCKS5 `CmdExpose` (0x07) stream (see 11.4) and
registers one or more routing keys. Each registration installs a
`forward.Backend` that streams new connections back to the operator over the
same stream; when the stream disconnects, the agent automatically revokes the
registration and the route disappears on the next advertisement.

**Wire protocol** (`internal/expose/protocol.go`): each expose frame is
`[u8 type][u64 stream_id BE][payload]`. The SOCKS5 transport delimits frames
with a `u32` big-endian length prefix (`[len:u32 BE][frame]`, same as the
route-watch framing). Frame types include `REGISTER`/`REGISTER_ACK`
(E -> M, JSON), `UNREGISTER`/`UNREGISTER_ACK`, `OPEN`/`OPEN_ACK` (per-stream
open driven by the agent), `DATA`, `CLOSE_WRITE`, `CLOSE`, and `PING`/`PONG`.
Outbound `DATA` frames are chunked at 16 KiB; the inbound frame read limit
defaults to 1 MiB.

**Routing label**: registrations advertise as `expose:<key>` so other agents
see them as ordinary forward routes. The exit agent ignores the label and
looks up the live backend by key. Collisions against config-defined or
journalled dynamic endpoints are rejected at registration time.

**Authentication**: expose inherits the SOCKS5 handshake auth performed before
command dispatch -- the same gate used by `CmdRouteWatch`/`CmdSnapshot`. There
is no separate token or password for the expose channel.

---

## 6.7 ICMP Echo Protocol

The ICMP echo (ping) feature allows sending ICMP echo requests through the mesh network to test connectivity to remote hosts. Unlike TCP streams which open connections directly, ICMP uses a session-based model with dedicated frame types and unprivileged ICMP sockets.

### Architecture

**Components:**

- **Handler** (`internal/icmp/handler.go`): Exit node ICMP processing, session management, sends real echo requests
- **Session** (`internal/icmp/session.go`): Per-session state with E2E encryption keys
- **Socket** (`internal/icmp/socket.go`): Platform-specific unprivileged ICMP socket operations
- **Config** (`internal/icmp/config.go`): Configuration and CIDR validation

### Session Lifecycle

ICMP uses a session model similar to TCP streams:

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                        ICMP SESSION LIFECYCLE                                │
│                                                                              │
│  Client              Ingress              Transit             Exit           │
│    │                    │                    │                  │            │
│    │  Ping request      │                    │                  │            │
│    │───────────────────>│                    │                  │            │
│    │                    │                    │                  │            │
│    │                    │    ICMP_OPEN       │                  │            │
│    │                    │───────────────────>│   ICMP_OPEN      │            │
│    │                    │                    │─────────────────>│            │
│    │                    │                    │                  │            │
│    │                    │                    │   ICMP_OPEN_ACK  │            │
│    │                    │   ICMP_OPEN_ACK    │<─────────────────│            │
│    │                    │<───────────────────│                  │            │
│    │                    │                    │                  │            │
│    │                    │    ICMP_ECHO       │   ICMP_ECHO      │ Real ICMP  │
│    │                    │~~~~~~~~~~~~~~~~~~~>│~~~~~~~~~~~~~~~~~>│~~~~~~~~~~~>│
│    │                    │                    │                  │            │
│    │                    │    ICMP_ECHO       │   ICMP_ECHO      │ Real reply │
│    │  Ping response     │<~~~~~~~~~~~~~~~~~~~│<~~~~~~~~~~~~~~~~~│<~~~~~~~~~~~│
│    │<───────────────────│                    │                  │            │
│    │                    │                    │                  │            │
│    │                    │    ICMP_CLOSE      │   ICMP_CLOSE     │            │
│    │                    │───────────────────>│─────────────────>│            │
│                                                                              │
│  Legend: ───> = Control frames  ~~~> = Echo data frames                      │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

**Session States:**

- `StateOpening`: Session being established, awaiting ICMP_OPEN_ACK
- `StateOpen`: Active session, can relay echo packets bidirectionally
- `StateClosed`: Session terminated

### Frame Protocol

#### ICMP_OPEN (0x40)

Request to establish an ICMP echo session to a target IP.

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ RequestID       │ 8      │ Stable correlation ID across hops        │
│ DestIPLen       │ 1      │ Destination IP length (4=IPv4, 16=IPv6)  │
│ DestIP          │ 4/16   │ Target IPv4 or IPv6 address              │
│ TTL             │ 1      │ Hop limit (decremented each hop)         │
│ PathLen         │ 1      │ Number of remaining hops in path         │
│ RemainingPath   │ 16*N   │ AgentIDs for remaining path              │
│ EphemeralPubKey │ 32     │ X25519 public key for E2E encryption     │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

#### ICMP_OPEN_ACK (0x41)

Confirms ICMP session established at exit node.

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ RequestID       │ 8      │ Matches RequestID from ICMP_OPEN         │
│ EphemeralPubKey │ 32     │ Exit's X25519 public key for E2E         │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

#### ICMP_OPEN_ERR (0x42)

ICMP session establishment failed.

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ RequestID       │ 8      │ Matches RequestID from ICMP_OPEN         │
│ ErrorCode       │ 2      │ Error code (50, 51, 52, or 18)           │
│ MessageLen      │ 1      │ Length of error message (max 255)        │
│ Message         │ N      │ Human-readable error description         │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

#### ICMP_ECHO (0x43)

Carries ICMP echo request or reply data. Used bidirectionally.

```
┌─────────────────┬────────┬──────────────────────────────────────────────┐
│ Field           │ Size   │ Description                                  │
├─────────────────┼────────┼──────────────────────────────────────────────┤
│ Identifier      │ 2      │ ICMP echo identifier                         │
│ Sequence        │ 2      │ ICMP sequence number                         │
│ IsReply         │ 1      │ 0 = request, 1 = reply                       │
│ SrcIPLen        │ 1      │ Source IP length (0=request, 4=IPv4,16=IPv6) │
│ SrcIP           │ 0/4/16 │ Source IP of echo reply (omitted on request) │
│ DataLen         │ 2      │ Length of encrypted payload                  │
│ Data            │ N      │ Encrypted echo payload (max 1472 bytes)      │
└─────────────────┴────────┴──────────────────────────────────────────────┘
```

The SrcIP field carries the actual source address of the ICMP echo reply as
observed by the exit node. This enables the ingress agent to surface the
responding IP to the user (e.g. to distinguish replies from load-balanced
destinations or from intermediate routers returning ICMP error messages).

#### ICMP_CLOSE (0x44)

Terminates an ICMP session.

```
┌─────────────────┬────────┬──────────────────────────────────────────┐
│ Field           │ Size   │ Description                              │
├─────────────────┼────────┼──────────────────────────────────────────┤
│ Reason          │ 1      │ Close reason:                            │
│                 │        │   0 = Normal termination                 │
│                 │        │   1 = Idle timeout                       │
│                 │        │   2 = Error occurred                     │
└─────────────────┴────────┴──────────────────────────────────────────┘
```

### E2E Encryption

ICMP echo uses the same E2E encryption as TCP streams:

1. Ingress generates ephemeral X25519 keypair
2. `ICMP_OPEN` includes ingress ephemeral public key
3. Exit generates ephemeral keypair, performs ECDH key exchange
4. `ICMP_OPEN_ACK` includes exit's ephemeral public key
5. Both sides derive session key via `DeriveSessionKey` (see section 6.4.1).
   The `streamOrRequestID` parameter carries the ICMP session's RequestID.
6. `ICMP_ECHO` data payloads encrypted with ChaCha20-Poly1305

Transit agents relay encrypted frames without decryption capability.

### Unprivileged ICMP Sockets

The implementation uses unprivileged ICMP sockets (no root required):

- **Linux**: Uses `udp4`/`udp6` network with `golang.org/x/net/icmp` package
  - Requires sysctl: `net.ipv4.ping_group_range="0 65535"` (disabled by default on most distros)
- **macOS/BSD**: Unprivileged ICMP available by default (no configuration required)
- **Windows**: Not supported. `NewSocketV4`/`NewSocketV6` are build-tagged
  (`socket_windows.go` / `socket_unix.go`). On Windows they return a clean
  `ErrNotSupported` error at socket creation, which the CLI surfaces as
  `ErrGeneralFailure`. Integration tests that exercise ICMP are skipped on Windows.

### Configuration

```yaml
icmp:
  enabled: true # Enable/disable ICMP feature
  max_sessions: 100 # Max concurrent sessions (0=unlimited)
  idle_timeout: 60s # Session cleanup timeout
  echo_timeout: 5s # Per-echo reply timeout
```

### Error Codes

| Code | Name                  | Description                           |
| ---- | --------------------- | ------------------------------------- |
| 50   | ICMP_DISABLED         | ICMP feature is disabled              |
| 51   | ICMP_DEST_NOT_ALLOWED | Destination not in allowed CIDRs      |
| 52   | ICMP_SESSION_LIMIT    | Max concurrent sessions reached       |
| 18   | GENERAL_FAILURE       | Socket creation or key exchange error |

### Package Structure

```
internal/icmp/
├── handler.go       # Exit node ICMP processing, session management
├── session.go       # Session state and lifecycle
├── socket.go        # Platform-specific ICMP socket operations
├── config.go        # Configuration and CIDR validation
├── doc.go           # Package documentation
├── handler_test.go  # Handler unit tests
├── session_test.go  # Session unit tests
├── socket_test.go   # Socket unit tests
└── config_test.go   # Config unit tests
```

---

## 6.8 Sleep Mode Protocol

Sleep mode enables mesh hibernation - agents can close all peer connections and enter an idle state, periodically polling for queued messages with randomized timing.

### Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           SLEEP MODE STATES                                 │
│                                                                             │
│  ┌─────────┐        Sleep()        ┌──────────┐       Poll Timer      ┌────────┐
│  │  AWAKE  │ ─────────────────────►│ SLEEPING │ ────────────────────► │POLLING │
│  │         │                       │          │                       │        │
│  │ Normal  │ ◄─────────────────────│   Idle   │ ◄──────────────────── │ Brief  │
│  │ Operation│        Wake()        │ No conns │    Poll Duration      │Reconnect
│  └─────────┘                       └──────────┘                       └────────┘
│                                                                             │
│  State Transitions:                                                         │
│  • AWAKE -> SLEEPING:  Sleep() called, all connections closed               │
│  • SLEEPING -> POLLING: Poll timer fires (jittered interval)                │
│  • POLLING -> SLEEPING: Poll duration ends, connections closed              │
│  • SLEEPING/POLLING -> AWAKE: Wake() called, connections restored           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Frame Types

| Type          | Value | Direction      | Description                                 |
| ------------- | ----- | -------------- | ------------------------------------------- |
| SLEEP_COMMAND | 0x50  | Flooded        | Instructs mesh to enter sleep mode          |
| WAKE_COMMAND  | 0x51  | Flooded        | Instructs mesh to exit sleep mode           |
| QUEUED_STATE  | 0x52  | Point-to-point | Delivers queued state to reconnecting agent |

### Frame Formats

#### SLEEP_COMMAND (0x50)

Flooded through the mesh to instruct all agents to hibernate.

```
┌─────────────────────────────────────────────────────────────────┐
│                      SLEEP_COMMAND Frame                        │
├─────────────────────────────────────────────────────────────────┤
│ OriginAgent   │ 16 bytes │ Agent that initiated the command     │
├─────────────────────────────────────────────────────────────────┤
│ CommandID     │ 8 bytes  │ Unique ID for deduplication          │
├─────────────────────────────────────────────────────────────────┤
│ Timestamp     │ 8 bytes  │ Unix timestamp when issued           │
├─────────────────────────────────────────────────────────────────┤
│ Signature     │ 64 bytes │ Ed25519 signature (zeros if unsigned)│
├─────────────────────────────────────────────────────────────────┤
│ SeenByCount   │ 1 byte   │ Number of agents in SeenBy list      │
├─────────────────────────────────────────────────────────────────┤
│ SeenBy[]      │ Variable │ Agent IDs for loop prevention        │
└─────────────────────────────────────────────────────────────────┘
```

#### WAKE_COMMAND (0x51)

Identical structure to SLEEP_COMMAND, instructs agents to resume normal operation.

#### QUEUED_STATE (0x52)

Sent to agents when they reconnect after sleeping, containing accumulated state updates.

```
┌─────────────────────────────────────────────────────────────────┐
│                       QUEUED_STATE Frame                        │
├─────────────────────────────────────────────────────────────────┤
│ RouteCount    │ 2 bytes  │ Number of route advertisements       │
├─────────────────────────────────────────────────────────────────┤
│ Routes[]      │ Variable │ Length-prefixed RouteAdvertise[]     │
├─────────────────────────────────────────────────────────────────┤
│ WithdrawCount │ 2 bytes  │ Number of route withdrawals          │
├─────────────────────────────────────────────────────────────────┤
│ Withdraws[]   │ Variable │ Length-prefixed RouteWithdraw[]      │
├─────────────────────────────────────────────────────────────────┤
│ NodeInfoCount │ 2 bytes  │ Number of node info updates          │
├─────────────────────────────────────────────────────────────────┤
│ NodeInfos[]   │ Variable │ Length-prefixed NodeInfoAdvertise[]  │
├─────────────────────────────────────────────────────────────────┤
│ HasSleepCmd   │ 1 byte   │ 1 if SleepCommand present            │
├─────────────────────────────────────────────────────────────────┤
│ SleepCmd      │ Variable │ SleepCommand (if HasSleepCmd=1)      │
├─────────────────────────────────────────────────────────────────┤
│ HasWakeCmd    │ 1 byte   │ 1 if WakeCommand present             │
├─────────────────────────────────────────────────────────────────┤
│ WakeCmd       │ Variable │ WakeCommand (if HasWakeCmd=1)        │
└─────────────────────────────────────────────────────────────────┘
```

### Sleep/Wake Command Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         SLEEP COMMAND PROPAGATION                           │
│                                                                             │
│  Operator triggers sleep via CLI or HTTP API on Agent1                      │
│                                                                             │
│  Agent1                    Agent2                    Agent3                 │
│    │                         │                         │                    │
│    │──SLEEP_COMMAND─────────►│                         │                    │
│    │  (SeenBy: [A1])         │──SLEEP_COMMAND─────────►│                    │
│    │                         │  (SeenBy: [A1,A2])      │                    │
│    │                         │                         │                    │
│    │    [Enter Sleep]        │    [Enter Sleep]        │    [Enter Sleep]   │
│    │                         │                         │                    │
│    X─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ X─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ X                      │
│       (connections closed)      (connections closed)                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Poll Cycle

During sleep mode, agents periodically reconnect to receive queued state updates:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              POLL CYCLE                                     │
│                                                                             │
│  Time    │ Agent State │ Actions                                            │
│  ────────┼─────────────┼──────────────────────────────────────────────────  │
│  T+0     │ SLEEPING    │ Waiting (jittered interval: 5min +/- 30%)          │
│  T+4.2m  │ POLLING     │ Poll timer fires, reconnect to peers               │
│  T+4.2m  │ POLLING     │ Receive queued RouteAdvertise, NodeInfo            │
│  T+4.2m  │ POLLING     │ Check for WAKE_COMMAND in queued state             │
│  T+4.7m  │ SLEEPING    │ Poll duration (30s) ends, disconnect               │
│  T+4.7m  │ SLEEPING    │ Schedule next poll (jittered)                      │
│  ...     │             │                                                    │
│                                                                             │
│  Jitter prevents predictable connection patterns                            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### State Queue

Awake agents queue state updates for sleeping peers:

- **Route Advertisements**: Queued with deduplication (same origin + sequence)
- **Route Withdrawals**: Queued with deduplication
- **Node Info Updates**: Latest per-origin only (older superseded)
- **Sleep/Wake Commands**: Latest command retained

Queue limits prevent memory exhaustion (configurable `max_queued_messages`).

### Command Deduplication

Sleep and wake commands use a seen-cache to prevent duplicate processing:

- Key: `(OriginAgent, CommandID)`
- TTL: 30 minutes
- Commands already seen are silently dropped

### Configuration

```yaml
sleep:
  enabled: true
  poll_interval: 5m # Base interval between polls
  poll_interval_jitter: 0.3 # +/- 30% variation
  poll_duration: 30s # How long to stay connected during poll
  persist_state: true # Survive agent restarts
  max_queued_messages: 1000 # Queue limit per peer
  auto_sleep_on_start: false # Start in sleep mode
  deterministic_windows: # Deterministic listening windows
    enabled: false # Use predictable windows derived from AgentID
    window_length: 30s # Duration of each listening window (max 30s)
    clock_tolerance: 5s # Clock drift tolerance
    epoch: "" # Reference point for window calculations (RFC3339, default: Unix epoch)
```

### State Persistence

When `persist_state` is enabled, sleep state is saved to `sleep_state.json`:

```json
{
  "state": 1,
  "sleep_start_time": "2026-01-19T10:30:00Z",
  "last_poll_time": "2026-01-19T10:35:00Z",
  "command_seq": 5
}
```

This allows agents to resume sleep mode after restart without losing context.

### Security Considerations

- Sleep/wake commands flood to all mesh agents - use signed commands in untrusted meshes
- Poll timing jitter prevents synchronized reconnect bursts across sleeping agents
- Persistent state is stored unencrypted - protect the data directory
- Signing private keys should only be distributed to authorized operators

### Command Signing and Verification

Sleep and wake commands support Ed25519 cryptographic signatures to prevent unauthorized mesh hibernation. This is separate from the X25519 keys used for stream encryption.

#### Signable Bytes Format

Commands are signed over a fixed 32-byte payload:

```
┌─────────────────────────────────────────────────────────────────┐
│                       SignableBytes (32 bytes)                  │
├─────────────────────────────────────────────────────────────────┤
│ OriginAgent   │ 16 bytes │ Agent ID that originated the command │
├─────────────────────────────────────────────────────────────────┤
│ CommandID     │ 8 bytes  │ Unique command identifier            │
├─────────────────────────────────────────────────────────────────┤
│ Timestamp     │ 8 bytes  │ Unix timestamp (seconds)             │
└─────────────────────────────────────────────────────────────────┘
```

#### Signature Generation

When the operator has a signing private key configured:

1. Construct SignableBytes from command fields (32 bytes)
2. Generate Ed25519 signature using the 64-byte private key
3. Include 64-byte signature in the frame

#### Signature Verification

When an agent receives a sleep/wake command:

1. Check if `signing_public_key` is configured
2. If not configured: accept all commands (backward compatible)
3. If configured:
   - Extract SignableBytes from the command (32 bytes)
   - Verify Ed25519 signature using the 32-byte public key
   - Reject commands with invalid signatures (logged at warn level)

#### Replay Protection

Commands include a Unix timestamp (seconds precision). Agents verify timestamps fall within a 5-minute window of the current time (`TimestampWindow` in the flooder configuration). Commands with timestamps outside this window are rejected to prevent replay attacks. The timestamp window is enforced unconditionally: commands with stale timestamps are rejected regardless of whether `signing_public_key` is configured. Signature verification is an additional gate that only applies when `signing_public_key` is set.

#### Key Configuration

```yaml
management:
  # Signing keys (for sleep/wake command authentication)
  signing_public_key: "..." # 64 hex chars (32 bytes) - ALL agents
  signing_private_key: "..." # 128 hex chars (64 bytes) - OPERATORS ONLY
```

#### CLI Commands

```bash
# Generate Ed25519 signing keypair
muti-metroo signing-key generate

# Derive public key from private key
muti-metroo signing-key public
```

### Deterministic Listening Windows

To enable coordinated reconnection during sleep mode, agents can calculate when other agents will be listening using deterministic window calculations. This allows an active agent to know precisely when a sleeping peer will open its listening window.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    DETERMINISTIC LISTENING WINDOWS                          │
│                                                                             │
│  CycleLength:  equal to sleep.poll_interval (default 5 minutes)             │
│  WindowLength: sleep.deterministic_windows.window_length                    │
│                (default 30s, max 30s)                                       │
│  Offset:       derived from AgentID (deterministic, unique per agent)       │
│                                                                             │
│  Timeline for Agent X (offset = 2m):                                        │
│  ├────────────┬────────────┬────────────┬────────────┬────────────┤         │
│  0m           2m        2m30s         5m          7m        7m30s           │
│  │            │==LISTEN===│            │           │==LISTEN===│            │
│  │            ▲           ▲            │           ▲           ▲            │
│  │        WindowStart  WindowEnd      │       WindowStart  WindowEnd        │
│  │                                     │                                    │
│  │◄──────── Cycle 1 ─────────────────►│◄──────── Cycle 2 ──────────────►│   │
│                                                                             │
│  Clock Tolerance: +/- 5 seconds around window for connection attempts       │
│                                                                             │
│  Because the cycle equals poll_interval, tuning poll_interval directly      │
│  shifts the cycle. Observers computing a peer's window must use the peer's  │
│  configured poll_interval, not the default 5 minutes.                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**WindowCalculator API:**

```go
// Calculate next listening window for a remote agent
calc := sleep.NewWindowCalculator(sleep.DefaultWindowConfig())
info := calc.GetWindowInfo(remoteAgentID, time.Now())

// info.Start, info.End - exact window boundaries
// info.SafeStart, info.SafeEnd - with clock tolerance
// info.TimeUntil - duration until window opens
// info.CurrentlyActive - true if currently in window
```

**Use Cases:**

- **Awake agent connecting to sleeping peer**: Calculate when peer's window opens, connect at optimal time
- **Mesh coordination**: Agents can predict each other's availability without explicit communication
- **Wake command delivery**: Route wake commands through agents whose windows overlap

### Package Structure

```
internal/sleep/
├── sleep.go        # Manager state machine, callbacks, persistence
├── queue.go        # StateQueue for sleeping peer message queuing
├── window.go       # Deterministic listening window calculator
├── sleep_test.go   # Sleep manager unit tests
└── window_test.go  # Window calculator unit tests
```

---

## 7. Stream Management

### 7.1 Stream Lifecycle

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          STREAM STATE MACHINE                               │
│                                                                             │
│                              ┌──────────┐                                   │
│                              │  CLOSED  │                                   │
│                              └────┬─────┘                                   │
│                                   │                                         │
│                    ┌──────────────┴──────────────┐                          │
│                    │                             │                          │
│               Send OPEN                     Recv OPEN                       │
│                    │                             │                          │
│                    ▼                             ▼                          │
│             ┌────────────┐                ┌────────────┐                    │
│             │  OPENING   │                │  OPENING   │                    │
│             │  (local)   │                │  (remote)  │                    │
│             └─────┬──────┘                └─────┬──────┘                    │
│                   │                             │                           │
│              Recv ACK                      Send ACK                         │
│                   │                             │                           │
│                   └──────────────┬──────────────┘                           │
│                                  ▼                                          │
│                           ┌────────────┐                                    │
│                       ┌──►│    OPEN    │◄──┐                                │
│                       │   └─────┬──────┘   │                                │
│                       │         │          │                                │
│                  STREAM_DATA    │     STREAM_DATA                           │
│                  (both ways)    │     (both ways)                           │
│                       │         │          │                                │
│                       └─────────┼──────────┘                                │
│                                 │                                           │
│              ┌──────────────────┼──────────────────┐                        │
│              │                  │                  │                        │
│         Recv CLOSE         Send CLOSE         STREAM_RESET                  │
│         (FIN_WRITE)        (FIN_WRITE)        (either side)                 │
│              │                  │                  │                        │
│              ▼                  ▼                  │                        │
│       ┌─────────────┐   ┌─────────────┐            │                        │
│       │ HALF_CLOSED │   │ HALF_CLOSED │            │                        │
│       │  (remote)   │   │   (local)   │            │                        │
│       └──────┬──────┘   └──────┬──────┘            │                        │
│              │                  │                  │                        │
│        Send CLOSE         Recv CLOSE               │                        │
│        (FIN_WRITE)        (FIN_WRITE)              │                        │
│              │                  │                  │                        │
│              └──────────────────┴──────────────────┘                        │
│                                │                                            │
│                                ▼                                            │
│                          ┌──────────┐                                       │
│                          │  CLOSED  │                                       │
│                          └──────────┘                                       │
│                                                                             │
│  Implementation Note: Half-closed states are tracked directionally as:      │
│  • StateHalfClosedLocal  - Local side initiated half-close                  │
│  • StateHalfClosedRemote - Remote side initiated half-close                 │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.2 Half-Close Semantics

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          HALF-CLOSE SEMANTICS                               │
│                                                                             │
│  Half-close allows one side to signal "done sending" while still receiving. │
│  Critical for protocols like SSH that use TCP FIN for session termination.  │
│                                                                             │
│  Example: SSH session exit                                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  SSH Client           Mesh Network           SSH Server             │    │
│  │       │                    │                      │                 │    │
│  │       │                    │         "exit"       │                 │    │
│  │       │                    │◄─────────────────────│                 │    │
│  │       │   STREAM_DATA      │                      │                 │    │
│  │       │◄───────────────────│                      │                 │    │
│  │       │                    │    Exit status + FIN │                 │    │
│  │       │                    │◄─────────────────────│                 │    │
│  │       │   STREAM_DATA      │                      │                 │    │
│  │       │◄───────────────────│                      │                 │    │
│  │       │   STREAM_CLOSE     │                      │                 │    │
│  │       │   (FIN_WRITE)      │                      │                 │    │
│  │       │◄───────────────────│                      │                 │    │
│  │       │                    │                      │                 │    │
│  │       │   Client processes final output           │                 │    │
│  │       │   STREAM_CLOSE     │                      │                 │    │
│  │       │   (FIN_WRITE)      │                      │                 │    │
│  │       │───────────────────►│──────────────────────►                 │    │
│  │       │                    │                      │                 │    │
│  │       │        Stream fully closed                │                 │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Without half-close: Server "close" would immediately kill stream,          │
│  potentially losing final output and exit status.                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.3 Resource Limits

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           RESOURCE LIMITS                                   │
│                                                                             │
│  Default limits (configurable):                                             │
│                                                                             │
│  ┌─────────────────────────────┬────────────┬───────────────────────────┐   │
│  │ Resource                    │ Default    │ Notes                     │   │
│  ├─────────────────────────────┼────────────┼───────────────────────────┤   │
│  │ Max streams per peer        │ 1,000      │ Incoming + outgoing       │   │
│  │ Max total streams           │ 10,000     │ All peers combined        │   │
│  │ Max pending opens           │ 100        │ STREAM_OPEN awaiting ACK  │   │
│  │ Stream open timeout         │ 30s        │ Time to receive ACK       │   │
│  │ Idle stream timeout         │ 5m         │ No data exchanged         │   │
│  │ Read buffer per stream      │ 256 KB     │                           │   │
│  │ Write buffer per stream     │ 256 KB     │                           │   │
│  └─────────────────────────────┴────────────┴───────────────────────────┘   │
│                                                                             │
│  When limits are reached:                                                   │
│  • New STREAM_OPEN receives STREAM_OPEN_ERR with RESOURCE_LIMIT             │
│  • Existing streams are not affected                                        │
│  • Log warning for monitoring                                               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 7.4 Proxy Chain Performance Characteristics

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                  PROXY CHAIN PERFORMANCE CHARACTERISTICS                    │
│                                                                             │
│  Note: max_hops only limits route advertisement propagation, NOT stream     │
│  path length. Stream paths are limited by the 30-second open timeout.       │
│                                                                             │
│  Recommended max hops by use case:                                          │
│  ┌────────────────────┬──────────────┬─────────────────────────────────┐    │
│  │ Use Case           │ Max Hops     │ Limiting Factor                 │    │
│  ├────────────────────┼──────────────┼─────────────────────────────────┤    │
│  │ Interactive SSH    │ 8-12 hops    │ Latency (5-50ms per hop)        │    │
│  │ Video Streaming    │ 6-10 hops    │ Buffering (256KB x hops)        │    │
│  │ Bulk Transfer      │ 12-16 hops   │ Throughput (16KB chunks)        │    │
│  │ High-latency WAN   │ 4-6 hops     │ 30s stream open timeout         │    │
│  └────────────────────┴──────────────┴─────────────────────────────────┘    │
│                                                                             │
│  Per-hop overhead:                                                          │
│  ┌────────────────────┬─────────────────────────────────────────────────┐   │
│  │ Latency            │ +1-5ms (LAN), +50-200ms (WAN)                   │   │
│  │ Memory             │ +256KB buffer per active stream                 │   │
│  │ CPU                │ Frame decode/encode at each relay               │   │
│  └────────────────────┴─────────────────────────────────────────────────┘   │
│                                                                             │
│  Protocol constants (non-configurable):                                     │
│  ┌────────────────────┬─────────────────────────────────────────────────┐   │
│  │ Max Frame Payload  │ 16 KB                                           │   │
│  │ Max Frame Size     │ 16,398 bytes (payload + 14-byte header)         │   │
│  │ Header Size        │ 14 bytes                                        │   │
│  │ Protocol Version   │ 0x01                                            │   │
│  │ Control Stream ID  │ 0 (reserved)                                    │   │
│  └────────────────────┴─────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 8. Routing System

### 8.1 Route Table Structure

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          ROUTE TABLE STRUCTURE                              │
│                                                                             │
│  Route {                                                                    │
│      Network     *net.IPNet    // CIDR prefix (e.g., 10.0.0.0/8)            │
│      NextHop     AgentID       // Immediate peer to forward to              │
│      OriginAgent AgentID       // Agent that advertised this route          │
│      Path        []AgentID     // Full path from here to origin             │
│      Metric      uint16        // Origin metric + 1 (not a hop count)       │
│      Sequence    uint64        // From origin's advertisement               │
│      LastUpdate  time.Time     // Updated on every refresh; route expires   │
│                                // when now - LastUpdate > RouteTTL          │
│      EncPath     *EncryptedData// Encrypted path payload for topology       │
│                                // compartmentalisation (see NODE_INFO_ADVERTISE) │
│  }                                                                          │
│                                                                             │
│  Table {                                                                    │
│      routes  map[string][]*Route // Keyed by CIDR string; each per-key list │
│                                  // is sorted by metric ascending           │
│      localID AgentID                                                        │
│      mu      sync.RWMutex                                                   │
│  }                                                                          │
│                                                                             │
│  Lookup iterates every CIDR key that contains the destination IP and keeps  │
│  the route with the longest prefix length; within a CIDR key, the metric-   │
│  sorted slice yields the lowest-metric entry first.                         │
│                                                                             │
│  In the current implementation OriginAgent is also the exit agent that      │
│  terminates the stream. The two concepts are kept distinct so that future   │
│  revisions can separate them (for example, advertising a route on behalf    │
│  of a third-party exit).                                                    │
│                                                                             │
│  Note: internal/routing/manager.go defines a separate, smaller type named   │
│  RouteEntry { Network *net.IPNet; Metric uint16 } used only as a payload    │
│  shape for advertisements; do not confuse it with Route above.              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

#### Domain Route Table

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        DOMAIN ROUTE TABLE STRUCTURE                         │
│                                                                             │
│  DomainRoute {                                                              │
│      Pattern     string        // "example.com" or "*.example.com"          │
│      IsWildcard  bool          // True if pattern starts with "*."          │
│      BaseDomain  string        // For wildcard: domain without "*."         │
│      NextHop     AgentID       // Immediate peer to forward to              │
│      OriginAgent AgentID       // Agent that advertised this route          │
│      Path        []AgentID     // Full path from here to origin             │
│      Metric      uint16        // Origin metric + 1 (not a hop count)       │
│      Sequence    uint64        // From origin's advertisement               │
│      LastUpdate  time.Time     // When this route was last updated          │
│  }                                                                          │
│                                                                             │
│  DomainTable {                                                              │
│      exactRoutes  map[string][]*DomainRoute  // "api.example.com" -> routes │
│      wildcardBase map[string][]*DomainRoute  // "example.com" -> wildcard   │
│      localID      AgentID                    // Local agent ID              │
│  }                                                                          │
│                                                                             │
│  Matching algorithm:                                                        │
│  1. Exact match: lookup domain in exactRoutes                               │
│  2. Wildcard match: split domain at first ".", lookup remainder in          │
│     wildcardBase (single-level wildcard only)                               │
│  3. Return route with lowest metric                                         │
│                                                                             │
│  Example lookups:                                                           │
│  • "api.example.com" with route "api.example.com" -> exact match            │
│  • "foo.example.com" with route "*.example.com" -> wildcard match           │
│  • "a.b.example.com" with route "*.example.com" -> NO MATCH (multi-level)   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 8.2 Longest Prefix Match

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        LONGEST PREFIX MATCH                                 │
│                                                                             │
│  Example route table on Agent1:                                             │
│                                                                             │
│  ┌──────────────────┬──────────┬──────────┬────────┬────────────────────┐   │
│  │ Prefix           │ NextHop  │ Origin   │ Metric │ Path               │   │
│  ├──────────────────┼──────────┼──────────┼────────┼────────────────────┤   │
│  │ 10.5.3.0/24      │ Agent2   │ Agent3   │ 2      │ [Agent2, Agent3]   │   │
│  │ 10.5.0.0/16      │ Agent2   │ Agent4   │ 3      │ [Agent2, Agent3,   │   │
│  │                  │          │          │        │  Agent4]           │   │
│  │ 10.0.0.0/8       │ Agent2   │ Agent3   │ 2      │ [Agent2, Agent3]   │   │
│  │ 0.0.0.0/0        │ Agent5   │ Agent5   │ 1      │ [Agent5]           │   │
│  └──────────────────┴──────────┴──────────┴────────┴────────────────────┘   │
│                                                                             │
│  Lookup(10.5.3.100):                                                        │
│    Matches: 10.5.3.0/24 (24 bits) ← Selected (longest prefix)               │
│    Result: Path=[Agent2, Agent3]                                            │
│                                                                             │
│  Tiebreaker rules: When multiple CIDRs of equal prefix length both contain  │
│  the destination IP, Lookup() selects the one with the lowest Metric. On a  │
│  metric tie, selection falls back to the lowest OriginAgent ID so the       │
│  result is deterministic across process restarts (Go map iteration order    │
│  is otherwise randomised).                                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 8.3 Route Expiration

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          ROUTE EXPIRATION                                   │
│                                                                             │
│  Routes have a TTL and expire if not refreshed.                             │
│                                                                             │
│  Timing parameters:                                                         │
│  ┌─────────────────────────────┬──────────┬─────────────────────────────┐   │
│  │ Parameter                   │ Default  │ Description                 │   │
│  ├─────────────────────────────┼──────────┼─────────────────────────────┤   │
│  │ Route TTL                   │ 5m       │ How long routes are valid   │   │
│  │ Advertise interval          │ 2m       │ How often to re-advertise   │   │
│  │ Route refresh/expiry sweep  │ same as  │ Sweep runs on the advertise │   │
│  │                             │ advertise│ ticker; no separate timer   │   │
│  │ Seen-cache cleanup          │ TTL/2    │ SeenCacheTTL / 2            │   │
│  └─────────────────────────────┴──────────┴─────────────────────────────┘   │
│                                                                             │
│  On peer disconnect:                                                        │
│  • Immediately remove all routes where NextHop = disconnected peer          │
│  • Don't wait for expiration                                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 9. Flood Protocol

### 9.1 Route Advertisement Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       ROUTE ADVERTISEMENT FLOW                              │
│                                                                             │
│  Agent3 is exit for 10.0.0.0/8                                              │
│                                                                             │
│       Agent3                   Agent2                   Agent1              │
│          │                        │                        │                │
│          │ ROUTE_ADVERTISE        │                        │                │
│          │ origin=Agent3          │                        │                │
│          │ seq=1                  │                        │                │
│          │ routes=[10.0.0.0/8]    │                        │                │
│          │ metric=0               │                        │                │
│          │ path=[Agent3]          │                        │                │
│          │ seenBy=[Agent3]        │                        │                │
│          │───────────────────────►│                        │                │
│          │                        │                        │                │
│          │                        │ Store route:           │                │
│          │                        │ 10.0.0.0/8             │                │
│          │                        │ nextHop=Agent3         │                │
│          │                        │ origin=Agent3          │                │
│          │                        │ metric=1               │                │
│          │                        │                        │                │
│          │                        │ ROUTE_ADVERTISE        │                │
│          │                        │ origin=Agent3          │                │
│          │                        │ seq=1                  │                │
│          │                        │ metric=1               │                │
│          │                        │ path=[Agent2,Agent3]   │                │
│          │                        │ seenBy=[Agent3,Agent2] │                │
│          │                        │───────────────────────►│                │
│          │                        │                        │                │
│          │                        │                        │ Store route    │
│          │                        │                        │ metric=2       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 9.2 Flood Algorithm

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           FLOOD ALGORITHM                                   │
│                                                                             │
│  OnReceive(ROUTE_ADVERTISE, fromPeer):                                      │
│                                                                             │
│      // 1. Loop prevention                                                  │
│      if self.ID in msg.SeenBy:                                              │
│          return  // Already processed this (self-origination subsumed by    │
│                 // SeenBy/Path checks)                                      │
│                                                                             │
│      // 1b. Reject advertisements claiming our own origin. We are the sole  │
│      //     authority on routes originating here, and local-origin entries  │
│      //     are never aged out by CleanupStaleRoutes, so an accepted forgery │
│      //     would be neither refreshable nor expirable.                     │
│      if msg.OriginAgent == self.ID:                                         │
│          return                                                             │
│                                                                             │
│      // 2. Duplicate suppression via seen-cache                             │
│      key := (msg.OriginAgent, msg.Sequence)                                 │
│      if seenCache.Contains(key):                                            │
│          return  // Already relayed this exact advertisement                │
│      seenCache.Add(key)  // Entries expire after SeenCacheTTL               │
│                                                                             │
│      // 3. Store routes (per-origin freshness enforced by AddRoute)         │
│      for route in msg.Routes:                                               │
│          entry := Route{                                                    │
│              Network:     route.Network,                                    │
│              NextHop:     fromPeer,                                         │
│              OriginAgent: msg.OriginAgent,                                  │
│              Path:        prepend(fromPeer, msg.Path),                      │
│              Metric:      route.Metric + 1,                                 │
│              Sequence:    msg.Sequence,                                     │
│              LastUpdate:  now(),                                            │
│          }                                                                  │
│          // AddRoute replaces the existing entry only when:                 │
│          //   new.Sequence > existing.Sequence, OR                          │
│          //   new.Sequence == existing.Sequence AND new.Metric < existing   │
│          routeTable.AddRoute(entry)                                         │
│                                                                             │
│      // 4. Forward to other peers (split horizon)                           │
│      msg.SeenBy = append(msg.SeenBy, self.ID)                               │
│      for peer in connectedPeers:                                            │
│          if peer != fromPeer && peer not in msg.SeenBy:                     │
│              send(peer, msg)  // Log errors, don't fail                     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Note on sequence recovery:** A peer that restarts without a persisted sequence
counter begins at 1. Receivers still accept updates because `AddRoute` compares the
new `(Sequence, Metric)` pair against the currently stored entry per route, not
against a global per-origin high-water mark. Seen-cache entries for the old
sequences expire after `SeenCacheTTL` (a fixed 5m default, independent of `advertise_interval`) or are
cleared explicitly when the peer link drops (`ClearSeenCacheForOrigin`).

**Seen-cache sizing:** The flood seen-cache holds entries of the form
`(OriginAgent, Sequence)`. Expected steady-state size is roughly
`agents * advertise_interval / SeenCacheTTL`. For the default 2m advertise interval
and ~5m TTL the per-agent overhead is small.

**Sequence ownership on re-advertisement:** When a peer connects, the full routing
table is pushed to it. Each origin's routes are re-advertised under that origin's
_stored_ sequence, never a locally minted one. Minting a local sequence for another
agent's routes makes the copy look strictly newer than anything that origin sends,
so every receiver rejects the origin's genuine advertisements, stops refreshing
`LastUpdate`, and drops the route at `route_ttl`. Because sequences seed from the
wall clock, the gap is unbounded and the origin never catches up, producing
recurring unreachability windows re-triggered by every peer-connect event. Only
routes the local agent originates carry a freshly minted sequence.

Routes are grouped by the sequence they were learned under, one advertisement
per group, rather than collapsed onto a single sequence per origin. Each group
is inherently within the 255-route wire limit because it arrived that way, and
grouping keeps every group deliverable: the receiver's (OriginAgent, Sequence)
seen-cache would discard all but one group if they shared a sequence.

**Ingress rate limiting:** `ROUTE_ADVERTISE` and `NODE_INFO_ADVERTISE` frames are
subject to a per-peer token-bucket rate limiter
(`FloodConfig.AdvertiseRateLimit` / `AdvertiseRateBurst`, defaulting to 10
advertisements per second with a burst of 50). Frames arriving from a peer that
exhausts its bucket are dropped and logged at warn level. The limiter is cleared
when a peer disconnects.

---

## 10. Peer Connection Management

### 10.1 Connection State Machine

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       CONNECTION STATE MACHINE                              │
│                                                                             │
│                           ┌────────────────┐                                │
│              ┌───────────►│  DISCONNECTED  │◄───────────────┐               │
│              │            └───────┬────────┘                │               │
│              │                    │                         │               │
│              │       Dial() or Accept()                     │               │
│              │                    │                    Max retries          │
│              │                    ▼                    exceeded             │
│              │            ┌────────────────┐                │               │
│              │            │  CONNECTING    │                │               │
│              │            └───────┬────────┘                │               │
│              │                    │                         │               │
│              │         Transport connected                  │               │
│              │                    │                         │               │
│              │                    ▼                         │               │
│              │            ┌────────────────┐                │               │
│              │      ┌────►│  HANDSHAKING   │                │               │
│              │      │     └───────┬────────┘                │               │
│              │      │             │                         │               │
│              │      │    PEER_HELLO exchange OK             │               │
│              │      │             │                         │               │
│              │      │             ▼                         │               │
│              │      │     ┌────────────────┐                │               │
│           Reconnect │     │   CONNECTED    │◄───┐           │               │
│           success   │     └───────┬────────┘    │           │               │
│              │      │             │             │           │               │
│              │      │        Error/Timeout  Keepalive       │               │
│              │      │             │          success        │               │
│              │      │             ▼             │           │               │
│              │      │     ┌────────────────┐    │           │               │
│              │      └─────│  RECONNECTING  │────┘           │               │
│              │            └───────┬────────┘                │               │
│              │                    │                         │               │
│              │                    └─────────────────────────┘               │
│              │                                                              │
│         Shutdown/                                                           │
│         Give up                                                             │
│              │                                                              │
│              └──────────────────────────────────────────────────────────────│
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.2 Handshake Protocol

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          HANDSHAKE PROTOCOL                                 │
│                                                                             │
│  Agent A (dialer)                              Agent B (listener)           │
│       │                                              │                      │
│       │ [Transport connects - QUIC/HTTP2/WS]         │                      │
│       │◄────────────────────────────────────────────►│                      │
│       │                                              │                      │
│       │ PEER_HELLO                                   │                      │
│       │ version=1                                    │                      │
│       │ agentID=A                                    │                      │
│       │ timestamp=1703001234                         │                      │
│       │ displayName="agent-A"                        │                      │
│       │ capabilities=[]  (reserved; empty today)     │                      │
│       │─────────────────────────────────────────────►│                      │
│       │                                              │                      │
│       │                                              │ Validate:            │
│       │                                              │ • Version compatible │
│       │                                              │ • AgentID expected?  │
│       │                                              │ • Timestamp fresh?   │
│       │                                              │                      │
│       │                                   PEER_HELLO_ACK                    │
│       │                                   version=1  │                      │
│       │                                   agentID=B  │                      │
│       │                                   timestamp=1703001235              │
│       │                                   displayName="agent-B"             │
│       │                                   capabilities=[]                   │
│       │◄─────────────────────────────────────────────│                      │
│       │                                              │                      │
│       │              Connection ready                │                      │
│                                                                             │
│  Handshake timeout: 10 seconds                                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.3 Reconnection Strategy

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        RECONNECTION STRATEGY                                │
│                                                                             │
│  Exponential backoff with jitter:                                           │
│                                                                             │
│  Parameters:                                                                │
│  ┌─────────────────────────────┬──────────┬─────────────────────────────┐   │
│  │ Parameter                   │ Default  │ Description                 │   │
│  ├─────────────────────────────┼──────────┼─────────────────────────────┤   │
│  │ Initial delay               │ 1s       │ First reconnect attempt     │   │
│  │ Max delay                   │ 60s      │ Cap on backoff              │   │
│  │ Multiplier                  │ 2.0      │ Exponential factor          │   │
│  │ Jitter                      │ 0.2      │ ±20% randomization          │   │
│  │ Max retries                 │ 0        │ 0 = infinite                │   │
│  └─────────────────────────────┴──────────┴─────────────────────────────┘   │
│                                                                             │
│  Retry sequence:                                                            │
│    Attempt 1:  1s    ± 0.2s  →  0.8s  - 1.2s                                │
│    Attempt 2:  2s    ± 0.4s  →  1.6s  - 2.4s                                │
│    Attempt 3:  4s    ± 0.8s  →  3.2s  - 4.8s                                │
│    ...                                                                      │
│    Attempt 7+: 60s   ± 12s   →  48s   - 72s  (capped)                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 10.4 Keepalive Mechanism

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         KEEPALIVE MECHANISM                                 │
│                                                                             │
│  Purpose:                                                                   │
│  • Detect dead connections                                                  │
│  • Maintain NAT/firewall mappings                                           │
│  • Measure round-trip time                                                  │
│                                                                             │
│  Parameters:                                                                │
│  ┌─────────────────────────────┬──────────┬─────────────────────────────┐   │
│  │ Parameter                   │ Default  │ Description                 │   │
│  ├─────────────────────────────┼──────────┼─────────────────────────────┤   │
│  │ Idle threshold              │ 5m       │ Send keepalive after idle   │   │
│  │ Timeout                     │ 90s      │ Declare dead if no response │   │
│  └─────────────────────────────┴──────────┴─────────────────────────────┘   │
│                                                                             │
│  Sent on StreamID 0 (control stream).                                       │
│  KEEPALIVE_ACK echoes the same timestamp for RTT calculation.               │
│                                                                             │
│  Dead-peer detection (either condition closes the connection):              │
│  • Inbound silence: no frame received for idle_threshold + timeout. The     │
│    clock is inbound-only -- outbound writes (including the keepalive        │
│    itself) never refresh it, so a blackholed peer cannot look alive.        │
│  • Unanswered ping: every keepalive must be answered by a KEEPALIVE_ACK     │
│    within timeout, matching on the echoed timestamp.                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 11. SOCKS5 Server

### 11.1 Supported Features

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        SOCKS PROXY SUPPORT                                  │
│                                                                             │
│  Protocol versions (auto-detected from first byte):                        │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ • SOCKS4  - CONNECT, IPv4                                           │    │
│  │ • SOCKS4a - CONNECT, IPv4 + domain names                           │    │
│  │ • SOCKS5  - CONNECT, UDP ASSOCIATE, IPv4, IPv6, domain names       │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  SOCKS5 features supported:                                                 │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ • CONNECT command (TCP proxy)                                       │    │
│  │ • UDP ASSOCIATE command (UDP proxy)                                 │    │
│  │ • IPv4 addresses                                                    │    │
│  │ • IPv6 addresses                                                    │    │
│  │ • Domain names (resolved at exit agent)                             │    │
│  │ • No authentication (method 0x00)                                   │    │
│  │ • Username/password authentication (method 0x02)                    │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Not supported:                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ • BIND command (incoming connections)                               │    │
│  │ • GSSAPI authentication (method 0x01)                               │    │
│  │ • SOCKS4 UDP (no UDP in SOCKS4 spec)                                │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 11.2 Error Mapping

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         SOCKS5 ERROR MAPPING                                │
│                                                                             │
│  Mapping performed by mapErrorToReply() in internal/socks5/handler.go.      │
│  A STREAM_OPEN_ERR from the exit is wrapped as *protocol.DialError at the   │
│  stream manager and its ErrorCode is consulted here before the Go-error     │
│  fallbacks:                                                                 │
│                                                                             │
│  ┌─────────────────────────────┬────────────────────────────────────────┐   │
│  │ Error source                │ SOCKS5 Reply                           │   │
│  ├─────────────────────────────┼────────────────────────────────────────┤   │
│  │ DialError: NO_ROUTE,        │ 0x04 Host unreachable                  │   │
│  │   HOST_UNREACHABLE,         │                                        │   │
│  │   DNS_ERROR                 │                                        │   │
│  │ DialError: NETWORK_         │ 0x03 Network unreachable               │   │
│  │   UNREACHABLE               │                                        │   │
│  │ DialError: CONNECTION_      │ 0x05 Connection refused                │   │
│  │   REFUSED                   │                                        │   │
│  │ DialError: CONNECTION_      │ 0x06 TTL expired (see note)            │   │
│  │   TIMEOUT, TTL_EXCEEDED     │                                        │   │
│  │ DialError: NOT_ALLOWED,     │ 0x02 Connection not allowed            │   │
│  │   PATH_NOT_ALLOWED          │                                        │   │
│  │ DialError: any other code   │ 0x01 General failure                   │   │
│  │ *net.DNSError               │ 0x04 Host unreachable                  │   │
│  │ *net.OpError with Timeout() │ 0x06 TTL expired (see note)            │   │
│  │ *net.OpError with Op="dial" │ 0x04 Host unreachable                  │   │
│  │ Any other error             │ 0x01 General failure                   │   │
│  └─────────────────────────────┴────────────────────────────────────────┘   │
│                                                                             │
│  Note: Mapping a dial timeout to SOCKS5 reply 0x06 (TTL expired) follows    │
│  curl's convention - SOCKS5 defines no dedicated connection-timeout reply.  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 11.3 Route-Watch Extension (CmdRouteWatch 0x05 / CmdSnapshot 0x06)

Two custom SOCKS5 commands let a client (Mutiauk) receive the agent's dashboard state over the same endpoint it uses for the data plane, replacing HTTP polling of `GET /api/dashboard`. They reuse the standard SOCKS5 greeting/auth and request layout; the request address is unused (clients send `ATYP=0x01, 0.0.0.0:0`, reserved for future options). The reply is a standard SOCKS5 reply: `REP=0x00` success, or `REP=0x07` (command not supported) when the agent has no route-watch provider configured, which lets clients detect older agents and fall back to polling.

After a `0x00` reply the server streams length-prefixed frames (server -> client only):

```
┌────────┬──────────────────┬─────────────────────────┐
│ type   │ length (u32 BE)  │ payload (length bytes)  │
│ 1 byte │                  │                         │
└────────┴──────────────────┴─────────────────────────┘
  type 0x01 = SNAPSHOT  -> payload is DashboardResponse JSON
  type 0x00 = HEARTBEAT -> length 0, no payload
```

- **CmdRouteWatch (0x05)**: long-lived. The server writes an initial SNAPSHOT, then a fresh SNAPSHOT whenever routes, mesh hosts, or suffixes change (coalesced over a ~250ms window), interleaved with periodic HEARTBEAT frames (~20s). Snapshots are last-write-wins -- only the latest is ever sent, never queued. The client treats prolonged silence (missed heartbeats) as a dead connection and reconnects with jittered backoff.
- **CmdSnapshot (0x06)**: one-shot. The server writes a single SNAPSHOT frame and closes. Used by the client's `route trace`.

The SNAPSHOT payload is byte-identical to the `GET /api/dashboard` body: both the HTTP handler and the route-watch path call the same `health.Server.BuildDashboardSnapshot()`, so they cannot drift. Frame payloads are capped at 8 MiB on both ends. Because route data rides the SOCKS5 connection, it inherits the SOCKS5 authentication (none or username/password) rather than the dashboard `http.token_hash`. The change signal is driven by the routing manager's subscription bus plus a direct hook on the mesh-host store (`hosts.Store.SetOnChange`), since host/suffix edits do not flow through the routing bus.

### 11.4 Expose Extension (CmdExpose 0x07)

A third custom SOCKS5 command runs an expose session over the raw, already-authenticated SOCKS5 stream (see 6.6), letting a client (Mutiauk) register ephemeral forward keys over the same endpoint it uses for the data plane. Like route-watch it reuses the SOCKS5 greeting/auth and request layout; the request address is unused (`ATYP=0x01, 0.0.0.0:0`). The reply is a standard SOCKS5 reply: `REP=0x00` success, or `REP=0x07` (command not supported) when the agent has no expose handler configured, which lets clients detect older agents.

Unlike route-watch, the post-reply stream is **bidirectional** -- both sides write -- and the connection stays open for the lifetime of the expose session. After the `0x00` reply both ends speak the expose framing: each `[u8 type][u64 stream_id BE][payload]` frame is delimited with a `u32` big-endian length prefix (`[len:u32 BE][frame]`). Both expose and route-watch use a u32 big-endian length prefix, but they place it differently: route-watch puts the type byte before the length (`[type:1][len:u32 BE][payload]`), whereas expose length-prefixes the entire `[type][stream_id][payload]` frame. The expose frame types, registration semantics, per-stream multiplexing, keepalive, and routing label are documented in 6.6. Frame payloads are capped at the manager's read limit (default 1 MiB). Authorization is the SOCKS5 handshake; there is no separate `http.token_hash` gate.

### 11.5 WebSocket Transport

The SOCKS5 server can optionally accept connections over WebSocket transport, enabling tunneling through environments where raw TCP/SOCKS5 traffic is blocked but HTTPS/WebSocket is permitted.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    SOCKS5 WEBSOCKET TRANSPORT                               │
│                                                                             │
│  TCP Transport (default):                                                   │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ Client ──[TCP]──> 127.0.0.1:1080 ──> SOCKS5 Handler                 │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  WebSocket Transport (optional):                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ Client ──[WebSocket]──> 0.0.0.0:8443/socks5 ──> SOCKS5 Handler      │    │
│  │                                                                     │    │
│  │ • Appears as HTTPS traffic to network filters                       │    │
│  │ • TLS termination at listener (or plaintext behind reverse proxy)  │    │
│  │ • Same SOCKS5 protocol, different transport                         │    │
│  │ • Plain splash page at "/"                                         │    │
│  │ • HTTP Basic Auth when socks5.auth is enabled                       │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Configuration:                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ socks5:                                                             │    │
│  │   websocket:                                                        │    │
│  │     enabled: true                                                   │    │
│  │     address: "0.0.0.0:8443"   # Listen address                      │    │
│  │     path: "/socks5"           # WebSocket upgrade path              │    │
│  │     plaintext: false          # TLS (true for reverse proxy)        │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

The WebSocket listener wraps websocket.Conn to implement net.Conn, allowing the same SOCKS5 handler to serve both TCP and WebSocket clients. Binary WebSocket messages carry SOCKS5 protocol data unchanged.

**Authentication Flow:**

When `socks5.auth.enabled` is true, the WebSocket endpoint enforces two layers of authentication:

1. **HTTP Basic Auth** - Validated before WebSocket upgrade. The WebSocket listener receives a `CredentialStore` from the agent and checks the `Authorization` header. Invalid or missing credentials return HTTP 401 Unauthorized.

2. **SOCKS5 Username/Password** - Standard RFC 1929 authentication after the WebSocket connection is established. The same credential store is used by the SOCKS5 handler.

Both use the same `socks5.auth.users` configuration. Compatible clients (like Mutiauk) automatically send credentials for both layers using the same username/password.

---

## 12. Data Plane

### 12.1 Frame Processing

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         FRAME PROCESSING                                    │
│                                                                             │
│  func processFrame(peer *PeerConn, frame *Frame):                           │
│      switch frame.Type:                                                     │
│                                                                             │
│      case STREAM_OPEN:                                                      │
│          handleStreamOpen(peer, frame)                                      │
│                                                                             │
│      case STREAM_OPEN_ACK, STREAM_OPEN_ERR:                                 │
│          handleStreamOpenResponse(peer, frame)                              │
│                                                                             │
│      case STREAM_DATA:                                                      │
│          entry := forwardTable.Lookup(peer.ID, frame.StreamID)              │
│          if entry == nil:                                                   │
│              send STREAM_RESET (unknown stream)                             │
│              return                                                         │
│          if entry.LocalConn != nil:                                         │
│              entry.LocalConn.Write(frame.Payload)  // Exit                  │
│          else:                                                              │
│              forward(entry.OutgoingPeer, frame)    // Transit               │
│                                                                             │
│      case STREAM_CLOSE:                                                     │
│          handleStreamClose(peer, frame)                                     │
│                                                                             │
│      case STREAM_RESET:                                                     │
│          handleStreamReset(peer, frame)                                     │
│                                                                             │
│      case ROUTE_ADVERTISE:                                                  │
│          floodProtocol.HandleAdvertise(peer, frame)                         │
│                                                                             │
│      case ROUTE_WITHDRAW:                                                   │
│          floodProtocol.HandleWithdraw(peer, frame)                          │
│                                                                             │
│      case KEEPALIVE:                                                        │
│          send KEEPALIVE_ACK with same timestamp                             │
│                                                                             │
│      case KEEPALIVE_ACK:                                                    │
│          updateRTT(peer, frame.Timestamp)                                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 12.2 Write Fairness

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          WRITE FAIRNESS                                     │
│                                                                             │
│  Problem: One stream (video) could monopolize the transport, starving       │
│  latency-sensitive streams (SSH).                                           │
│                                                                             │
│  Solution for HTTP/2 and WebSocket (single transport stream):               │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  // Maximum data per frame                                          │    │
│  │  const maxFramePayload = 16384  // 16 KB                            │    │
│  │                                                                     │    │
│  │  // Writer loop with round-robin                                    │    │
│  │  for {                                                              │    │
│  │      streams := scheduler.GetReadyStreams()                         │    │
│  │      for _, stream := range streams {                               │    │
│  │          // Send at most one frame per stream per round             │    │
│  │          data := stream.queue.Read(maxFramePayload)                 │    │
│  │          frame := createDataFrame(stream.id, data)                  │    │
│  │          transport.Write(frame)                                     │    │
│  │      }                                                              │    │
│  │  }                                                                  │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  QUIC transport: Not needed (native per-stream fairness).                   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 13. Configuration

### 13.1 Configuration File (YAML)

```yaml
# ==============================================================================
# Mesh Agent Configuration
# ==============================================================================

# ------------------------------------------------------------------------------
# Agent Identity
# ------------------------------------------------------------------------------
agent:
  # Agent ID: "auto" generates on first run, or specify hex string
  id: "auto"

  # Human-readable display name (Unicode allowed)
  # If not set, falls back to agent ID for display
  display_name: ""

  # Directory for persistent state (optional when private_key is set below)
  data_dir: "./data"

  # Logging
  log_level: "info" # debug, info, warn, error
  log_format: "text" # text, json

  # Delay all network activity (listeners, peers, SOCKS5, ...) for this duration
  # after the process starts. 0 = no delay (default).
  startup_delay: 0s

  # Optional: embed the X25519 identity keypair directly in config for
  # single-file deployments (data_dir becomes optional when this is set)
  # private_key: "<64-char hex, 32 bytes>"
  # public_key:  "<optional, derived from private_key if omitted>"

# ------------------------------------------------------------------------------
# Protocol Identifiers
# Customize identifiers that appear in network traffic
# Set values to empty strings to disable custom identifiers
# ------------------------------------------------------------------------------
protocol:
  alpn: "muti-metroo/1" # ALPN for QUIC/TLS (empty to disable)
  http_header: "X-Muti-Metroo-Protocol" # HTTP/2 header (empty to disable)
  ws_subprotocol: "muti-metroo/1" # WebSocket subprotocol (empty to disable)

# ------------------------------------------------------------------------------
# Transport Listeners
# ------------------------------------------------------------------------------
listeners:
  # QUIC listener (best performance)
  - transport: quic
    address: "0.0.0.0:4433"
    tls:
      # Option 1: File paths
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"
      client_ca: "./certs/ca.crt" # Optional: require client certs

      # Option 2: Inline PEM content (takes precedence over file paths)
      # Useful for secrets management or single-file configs
      # cert_pem: |
      #   -----BEGIN CERTIFICATE-----
      #   ...
      #   -----END CERTIFICATE-----
      # key_pem: |
      #   -----BEGIN PRIVATE KEY-----
      #   ...
      #   -----END PRIVATE KEY-----

  # HTTP/2 listener (TCP fallback)
  - transport: h2
    address: "0.0.0.0:8443"
    path: "/mesh"
    tls:
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"

  # WebSocket listener (maximum compatibility)
  - transport: ws
    address: "0.0.0.0:443"
    path: "/mesh"
    tls:
      cert: "./certs/agent.crt"
      key: "./certs/agent.key"

# ------------------------------------------------------------------------------
# Peer Connections
# ------------------------------------------------------------------------------
peers:
  # QUIC peer
  - id: "abc123..." # Expected peer AgentID
    transport: quic
    address: "192.168.1.50:4433"
    tls:
      ca: "./certs/peer-ca.crt"
      strict: true # Enable CA verification

  # WebSocket peer through proxy
  - id: "ghi789..."
    transport: ws
    address: "wss://relay.example.com:443/mesh"
    proxy: "http://proxy.corp.local:8080"
    proxy_auth:
      username: "${PROXY_USER}"
      password: "${PROXY_PASS}"

# ------------------------------------------------------------------------------
# SOCKS5 Server
# ------------------------------------------------------------------------------
socks5:
  enabled: true
  address: "127.0.0.1:1080"

  # Authentication (optional)
  auth:
    enabled: false
    users:
      - username: "user1"
        password: "pass1"

  # Limits
  max_connections: 1000

  # WebSocket transport (optional)
  # Enables SOCKS5 over WebSocket for environments where raw TCP is blocked
  websocket:
    enabled: false
    address: "0.0.0.0:8443" # Listen address for WebSocket connections
    path: "/socks5" # WebSocket upgrade path
    plaintext: false # Set true when behind reverse proxy (nginx/Caddy)

# ------------------------------------------------------------------------------
# Exit Configuration
# ------------------------------------------------------------------------------
exit:
  enabled: true

  # CIDR routes to advertise
  routes:
    - "10.0.0.0/8"
    - "192.168.0.0/16"

  # DNS settings
  dns:
    servers:
      - "8.8.8.8:53"
      - "1.1.1.1:53"
    timeout: 5s

# ------------------------------------------------------------------------------
# Routing
# ------------------------------------------------------------------------------
routing:
  advertise_interval: 2m
  node_info_interval: 2m # Node info advertisement (defaults to advertise_interval)
  route_ttl: 5m
  max_hops: 16

# ------------------------------------------------------------------------------
# Connection Tuning
# ------------------------------------------------------------------------------
connections:
  # Keepalive
  idle_threshold: 5m
  timeout: 90s
  keepalive_jitter: 0.2 # +/- jitter fraction on keepalive timing (0.0-1.0)

  # Reconnection
  reconnect:
    initial_delay: 1s
    max_delay: 60s
    multiplier: 2.0
    jitter: 0.2
    max_retries: 0 # 0 = infinite

# ------------------------------------------------------------------------------
# Resource Limits
# ------------------------------------------------------------------------------
limits:
  max_streams_per_peer: 1000
  max_streams_total: 10000
  max_pending_opens: 100
  stream_open_timeout: 30s
  buffer_size: 262144 # 256 KB

# ------------------------------------------------------------------------------
# HTTP API Server
# ------------------------------------------------------------------------------
http:
  enabled: true
  address: ":8080"
  read_timeout: 10s
  write_timeout: 10s
  token_hash: "" # bcrypt hash of bearer token (empty = no auth)

  # Endpoint control flags
  minimal: false # When true, only /health, /healthz, /ready are enabled
  pprof: false # /debug/pprof/* endpoints (disable in production)
  dashboard: true # /api/* endpoints
  remote_api: true # /agents/* endpoints

# ------------------------------------------------------------------------------
# Management Commands
# ------------------------------------------------------------------------------
shell:
  enabled: false # Disabled by default for security
  whitelist: [] # Empty = no commands allowed; ["*"] = all (testing only!)
  password_hash: "" # bcrypt hash of shell password
  timeout: 60s # Default command execution timeout

# ------------------------------------------------------------------------------
# File Transfer
# ------------------------------------------------------------------------------
file_transfer:
  enabled: false # Disabled by default for security
  max_file_size: 524288000 # Maximum file size in bytes (500 MB)
  allowed_paths: [] # Allowed path prefixes (empty = all absolute paths)
  password_hash: "" # bcrypt hash of file transfer password

# ------------------------------------------------------------------------------
# UDP Relay (SOCKS5 UDP ASSOCIATE support at exit nodes)
# ------------------------------------------------------------------------------
udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
  max_datagram_size: 1472

# ------------------------------------------------------------------------------
# ICMP Echo (ping through the mesh)
# ------------------------------------------------------------------------------
icmp:
  enabled: true
  max_sessions: 100
  idle_timeout: 60s
  echo_timeout: 5s

# ------------------------------------------------------------------------------
# Port Forwarding (reverse tunnel)
# ------------------------------------------------------------------------------
forward:
  endpoints: []
  # - key: "web-server"
  #   target: "localhost:3000"
  listeners: []
  # - key: "web-server"
  #   address: ":8080"
  #   max_connections: 100

# ------------------------------------------------------------------------------
# Sleep Mode (mesh hibernation)
# ------------------------------------------------------------------------------
sleep:
  enabled: false
  poll_interval: 5m
  poll_interval_jitter: 0.3
  poll_duration: 30s
  persist_state: true
  max_queued_messages: 1000
  auto_sleep_on_start: false
  deterministic_windows:
    enabled: false
    window_length: 30s
    clock_tolerance: 5s
    epoch: "" # RFC3339; empty = Unix epoch

# ------------------------------------------------------------------------------
# Management Key Encryption
# Encrypt mesh topology data for privacy
# ------------------------------------------------------------------------------
management:
  # Management public key (64-character hex string)
  # When set, NodeInfo and route paths are encrypted before flooding
  # All agents in mesh should have the SAME public key
  # Generate with: muti-metroo management-key generate
  public_key: ""

  # Management private key (64-character hex string)
  # Only set on operator/management nodes that need to view topology
  # NEVER distribute to field agents
  # When set, this node can decrypt NodeInfo and view mesh topology
  private_key: ""

  # Signing keys (for sleep/wake command authentication)
  signing_public_key: "" # 64 hex chars (32 bytes) - ALL agents
  signing_private_key: "" # 128 hex chars (64 bytes) - OPERATORS ONLY
```

### 13.2 Environment Variable Substitution

Configuration values support environment variable substitution:

```yaml
peers:
  - id: "${PEER_ID}"
    address: "${PEER_ADDRESS}"
    proxy_auth:
      password: "${PROXY_PASSWORD:-default}"
```

Syntax:

- `${VAR}` - Substitute variable value
- `${VAR:-default}` - Use default if variable not set

### 13.3 Command-Line Interface

```bash
# Interactive setup wizard
muti-metroo setup

# Initialize new agent
muti-metroo init --data-dir ./data

# Run agent
muti-metroo run --config ./config.yaml

# Show status (via HTTP API)
muti-metroo status -a localhost:8080

# List peers
muti-metroo peers

# List routes
muti-metroo routes

# Mesh connectivity testing
muti-metroo mesh-test                # Test connectivity to all agents
muti-metroo mesh-test --json         # JSON output

# Connectivity probe (listener reachability verification)
muti-metroo probe <address>          # Dial a listener and verify handshake
muti-metroo probe listen             # Start a probe listener for inbound testing

# ICMP ping through mesh
muti-metroo ping <target-agent-id> <destination-ip>

# Certificate management
muti-metroo cert ca -n "My CA" -o ./certs -d 365
muti-metroo cert agent -n "agent-1" --ca ./certs/ca.crt
muti-metroo cert client -n "client-1"
muti-metroo cert info ./certs/ca.crt

# Management commands (execute command on remote agent)
# Authorized by the mesh management Ed25519 signing key; no per-command password.
muti-metroo shell <target-agent-id> <command> [args...]
muti-metroo shell -a 192.168.1.10:8080 abc123def456 hostname
muti-metroo shell --tty abc123def456 bash

# File transfer
muti-metroo upload <target-agent-id> <local-path> <remote-path>
muti-metroo download <target-agent-id> <remote-path> <local-path>

# Dynamic route management
muti-metroo route add 10.0.0.0/8
muti-metroo route remove 10.0.0.0/8
muti-metroo route list

# Dynamic forward listener management
muti-metroo forward add <key> <address>
muti-metroo forward remove <key>
muti-metroo forward list

# Dynamic peer management (ephemeral; tell a running agent to dial a new peer)
muti-metroo peer add <address> [--transport quic|h2|ws] [--id <agent-id>]
muti-metroo peer remove <address>
muti-metroo peer list

# Dynamic display name management
muti-metroo display-name set "my-agent"
muti-metroo display-name get

# Password hash generation (for SOCKS5, shell, file transfer auth)
muti-metroo hash                     # Interactive prompt
muti-metroo hash "password"          # From argument
muti-metroo hash --cost 12           # Custom bcrypt cost

# Management key encryption (X25519 for mesh topology encryption)
muti-metroo management-key generate  # Generate keypair
muti-metroo management-key public    # Derive public from private

# Signing key management (Ed25519 for sleep/wake authentication)
muti-metroo signing-key generate     # Generate Ed25519 keypair
muti-metroo signing-key public       # Derive public from private

# Sleep/wake commands
muti-metroo sleep                    # Put mesh to sleep
muti-metroo wake                     # Wake mesh
muti-metroo sleep-status             # Check sleep status

# Service management
muti-metroo service install -c /path/to/config.yaml
muti-metroo service uninstall
muti-metroo service status
```

---

## 14. Security

### 14.1 Transport Security

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         TRANSPORT SECURITY                                  │
│                                                                             │
│  Layered Security Model:                                                    │
│  • Primary: E2E encryption (X25519 + ChaCha20-Poly1305) protects payloads   │
│  • Secondary: TLS 1.3 encrypts transport (defense-in-depth)                 │
│                                                                             │
│  Default behavior (no TLS config):                                          │
│  • Auto-generate self-signed ECDSA certificate on startup                   │
│  • No certificate verification (safe because E2E protects payload)          │
│  • Certificates regenerated each startup (ephemeral)                        │
│                                                                             │
│  All transports use TLS:                                                    │
│                                                                             │
│  ┌─────────────────┬─────────────────────────────────────────────────────┐  │
│  │ Transport       │ TLS Implementation                                  │  │
│  ├─────────────────┼─────────────────────────────────────────────────────┤  │
│  │ QUIC            │ TLS 1.3 (mandatory, built into QUIC)                │  │
│  │ HTTP/2          │ TLS 1.3 (MinVersion enforced)                       │  │
│  │ WebSocket       │ TLS 1.3 (WSS, MinVersion enforced)                  │  │
│  └─────────────────┴─────────────────────────────────────────────────────┘  │
│                                                                             │
│  Strict mode (tls.strict: true):                                            │
│  • CA-based: Validate against trusted CA certificate                        │
│  • Mutual TLS: Both sides present and validate certificates                 │
│                                                                             │
│  TLS ClientHello profiles (tls.fingerprint.preset):                         │
│  • Client-side only (uTLS-based ClientHello selection)                      │
│  • Presets: "chrome", "firefox", "safari", "edge", "ios",                   │
│    "android", "random", "go" (explicit stdlib), "disabled"/"" (default)     │
│  • Applies to HTTP/2 and WebSocket client dials                             │
│  • Not supported for QUIC transport (uses built-in quic-go TLS)             │
│  • Disabled by default; some WAFs reject browser-like ClientHello           │
│    combined with non-browser HTTP                                           │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 14.2 Trust Model

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            TRUST MODEL                                      │
│                                                                             │
│  Direct trust:                                                              │
│  • Agent trusts directly connected peers                                    │
│  • Verified via AgentID handshake (TLS verification optional)               │
│                                                                             │
│  Transitive trust:                                                          │
│  • Agent trusts route advertisements from direct peers                      │
│  • These include routes from agents further in the mesh                     │
│  • No direct verification of distant agents                                 │
│                                                                             │
│  What transit agents can see:                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │ Visible:                                                            │    │
│  │ • Destination IP/port (in STREAM_OPEN)                              │    │
│  │ • Traffic volume and timing                                         │    │
│  │ • Path information                                                  │    │
│  │                                                                     │    │
│  │ Not visible (encrypted end-to-end):                                 │    │
│  │ • Payload content (encrypted with ChaCha20-Poly1305)                │    │
│  │ • Application-layer data                                            │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  End-to-end encryption (built-in):                                          │
│  • X25519 key exchange during stream open (ephemeral keys in frames)        │
│  • ChaCha20-Poly1305 AEAD for all STREAM_DATA payloads                      │
│  • Transit agents cannot decrypt stream data                                │
│  • Each stream has unique session keys derived via ECDH                     │
│                                                                             │
│  Key exchange flow:                                                         │
│  1. Ingress generates ephemeral X25519 keypair                              │
│  2. Ingress sends public key in STREAM_OPEN                                 │
│  3. Exit generates ephemeral X25519 keypair                                 │
│  4. Exit sends public key in STREAM_OPEN_ACK                                │
│  5. Both compute shared secret: ECDH(local_private, remote_public)          │
│  6. Session key derived via HKDF-SHA256 per section 6.4.1                   │
│     (salt binds streamOrRequestID and both ephemeral public keys; info      │
│     string is "muti-metroo-e2e-v1"; output is a 32-byte ChaCha20-Poly1305 key)    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 14.3 Configuration Security

Sensitive configuration values are automatically redacted in logs:

```go
// Redacted fields:
// - tls.key
// - tls.key_pem
// - peers[].proxy_auth.password
// - peers[].tls.key
// - peers[].tls.key_pem
// - listeners[].tls.key
// - listeners[].tls.key_pem
// - socks5.auth.users[].password
// - socks5.auth.users[].password_hash
// - agent.private_key
// - shell.password_hash
// - file_transfer.password_hash
// - management.private_key
// - management.signing_private_key
// - http.token_hash

config.String()       // Returns YAML with [REDACTED] for sensitive values
config.StringUnsafe() // Returns full YAML (use only for debugging)
config.Redacted()     // Returns copy with redacted values
```

### 14.4 Operational Security Considerations

**Timestamp replay protection.** The 5-minute timestamp validity window for
sleep/wake commands (section 6.8) is enforced unconditionally: commands with
timestamps outside the window are rejected regardless of whether
`signing_public_key` is configured. An attacker who captures a sleep or wake
command in transit can no longer replay it indefinitely. Signature verification
still additionally requires `signing_public_key`.

Recommendation: configure `signing_public_key` on every agent (even if
`signing_private_key` is only distributed to operators) so that unsigned commands
are rejected entirely.

**Data directory protection.** Sleep-mode state (`sleep_state.json`) and, when
file-based identity is used, the agent's X25519 private key are stored unencrypted on
disk. Set `data_dir` to mode `0700` and own it by the service user.

**Metadata leakage at transit.** Although E2E encryption (ChaCha20-Poly1305) prevents
transit agents from reading stream payloads, they still observe:

- The destination IP/port (in STREAM_OPEN, before the encrypted session begins)
- Traffic timing and volume per stream
- The mesh path taken (from RemainingPath / Path)

Operators concerned about connection metadata exposure should avoid mixing trusted and untrusted
transit agents on the same path.

---

## 15. Observability

### 15.1 HTTP API Endpoints

HTTP endpoints are exposed when `http.enabled: true`:

**Health & Monitoring:**

| Endpoint   | Method | Description                         |
| ---------- | ------ | ----------------------------------- |
| `/health`  | GET    | Basic liveness probe (returns "OK") |
| `/healthz` | GET    | Detailed health with JSON stats     |
| `/ready`   | GET    | Readiness probe (returns "READY")   |

**JSON API:**

| Endpoint              | Method   | Description                                                                           |
| --------------------- | -------- | ------------------------------------------------------------------------------------- |
| `/api/topology`       | GET      | Topology data: agents (incl. `advertised_listeners`) and connections (incl. `dialer`) |
| `/api/dashboard`      | GET      | Dashboard overview (agent info, stats, peers, routes, hosts)                          |
| `/api/nodes`          | GET      | Detailed node info listing for all known agents                                       |
| `/api/mesh-test`      | GET/POST | Mesh connectivity test (GET = cached 30s; POST = forces fresh test)                   |
| `/api/probe`          | POST     | Probe a Muti Metroo listener (dial + PEER_HELLO handshake) from the local agent             |
| `/api/setup-defaults` | GET      | Suggested defaults for the in-browser setup UI                                        |

**Distributed Status:**

| Endpoint                           | Method | Description                                            |
| ---------------------------------- | ------ | ------------------------------------------------------ |
| `/agents`                          | GET    | List all known agents in the mesh                      |
| `/agents/{agent-id}`               | GET    | Get status from specific agent                         |
| `/agents/{agent-id}/routes`        | GET    | Get route table from specific agent                    |
| `/agents/{agent-id}/peers`         | GET    | Get peer list from specific agent                      |
| `/agents/{agent-id}/shell`         | GET    | WebSocket management commands on remote agent          |
| `/agents/{agent-id}/icmp`          | GET    | WebSocket ICMP ping sessions                           |
| `/agents/{agent-id}/file/upload`   | POST   | Upload file to remote agent                            |
| `/agents/{agent-id}/file/download` | POST   | Download file from remote agent                        |
| `/agents/{agent-id}/file/browse`   | POST   | Browse filesystem on remote agent                      |
| `/agents/{agent-id}/probe`         | POST   | Probe a listener from a remote agent's perspective     |
| `/agents/{agent-id}/route/trace`   | POST   | Run a route trace from a remote agent's routing tables |

**Diagnostics:**

| Endpoint       | Method | Description                                                        |
| -------------- | ------ | ------------------------------------------------------------------ |
| `/route/trace` | POST   | Explain which CIDR/domain route the local agent picks for a target |

**Management:**

| Endpoint                                | Method | Description                                              |
| --------------------------------------- | ------ | -------------------------------------------------------- |
| `/routes/advertise`                     | POST   | Trigger immediate route advertisement                    |
| `/routes/manage`                        | POST   | Add, remove, or list dynamic CIDR exit routes            |
| `/agents/{id}/routes/manage`            | POST   | Manage routes on a remote agent                          |
| `/forward/manage`                       | POST   | Add, remove, or list dynamic forward listeners           |
| `/agents/{id}/forward/manage`           | POST   | Manage forward listeners on a remote agent               |
| `/forward/endpoints/manage`             | POST   | Add, remove, or list dynamic forward endpoints           |
| `/agents/{id}/forward/endpoints/manage` | POST   | Manage forward endpoints on a remote agent               |
| `/peers/manage`                         | POST   | Add, remove, or list dynamic peer connections            |
| `/agents/{id}/peers/manage`             | POST   | Manage peer connections on a remote agent                |
| `/display-name/manage`                  | POST   | Set or get agent display name dynamically                |
| `/agents/{id}/display-name/manage`      | POST   | Manage display name on a remote agent                    |
| `/hosts/manage`                         | POST   | Add, remove, or list mesh-host entries (local-only)      |
| `/hosts/suffixes/manage`                | POST   | Add, remove, or list mesh-host TLD suffixes (local-only) |

**Sleep Mode:**

| Endpoint        | Method | Description                                                 |
| --------------- | ------ | ----------------------------------------------------------- |
| `/sleep`        | POST   | Trigger mesh-wide sleep (signed if signing_private_key set) |
| `/wake`         | POST   | Trigger mesh-wide wake (signed if signing_private_key set)  |
| `/sleep/status` | GET    | Get current sleep status                                    |

**Debugging (pprof):**

| Endpoint               | Method | Description                    |
| ---------------------- | ------ | ------------------------------ |
| `/debug/pprof/`        | GET    | pprof index                    |
| `/debug/pprof/cmdline` | GET    | Running program's command line |
| `/debug/pprof/profile` | GET    | CPU profile                    |
| `/debug/pprof/symbol`  | GET    | Symbol lookup                  |
| `/debug/pprof/trace`   | GET    | Execution trace                |

**Example `/healthz` response:**

```json
{
  "status": "healthy",
  "running": true,
  "peer_count": 3,
  "stream_count": 5,
  "route_count": 10,
  "socks5_running": true,
  "exit_handler_running": false,
  "can_sign_management": true,
  "accepts_signed_management": true
}
```

`can_sign_management` is `true` when the agent holds `management.signing_private_key` and can therefore initiate shell, file-transfer, dynamic-mutation, and sleep/wake requests. `accepts_signed_management` is `true` when the agent holds `management.signing_public_key` and will verify and execute signed requests from operator agents. Web dashboards and the CLI use these flags to enable or disable management UI affordances without probing each feature individually.

**Authentication on these endpoints.** Every endpoint listed above (other than `/health`, `/healthz`, `/ready`, `/`, and `/logo.png`) requires the bearer token from `http.token_hash` when that field is configured. In addition, every mutation action (`add`, `remove`, `set`, `set_comment`, etc.) on the `*/manage` and `/sleep`-family endpoints carries an Ed25519 management signature: the local agent must hold `management.signing_private_key` to _initiate_ the request, and the target agent must hold `management.signing_public_key` to _verify_ and execute it. Read-only actions (`list`, `get`, `route trace`, `mesh-test`) and the WebSocket shell / icmp endpoints follow the same model -- the WebSocket envelopes carry the signature inside the first message rather than in the HTTP request.

### 15.2 Structured Logging

Logging uses Go's `slog` package with configurable levels and formats.

**Log Levels:** `debug`, `info`, `warn`, `error`

**Log Formats:** `text`, `json`

**Standard Log Fields:**

```go
const (
    KeyPeerID     = "peer_id"
    KeyStreamID   = "stream_id"
    KeyRequestID  = "request_id"
    KeyAddress    = "address"
    KeyTransport  = "transport"
    KeyRoute      = "route"
    KeyHops       = "hops"
    KeyError      = "error"
    KeyComponent  = "component"
    KeyAgentID    = "agent_id"
    KeyRemoteAddr = "remote_addr"
    KeyLocalAddr  = "local_addr"
    KeyDuration   = "duration"
    KeyCount      = "count"
)
```

---

## 16. Operations

### 16.1 Service Installation

The agent can be installed as a system service.

**Supported Platforms:**

- Linux (systemd)
- macOS (launchd)
- Windows (Service Control Manager)

**Linux Installation:**

```bash
# Via setup wizard
sudo muti-metroo setup

# Manual installation handled by wizard
# Creates /etc/systemd/system/muti-metroo.service
```

**Systemd Service Features:**

- Restart on failure (5s delay)
- Security hardening (NoNewPrivileges, ProtectSystem, PrivateTmp)
- Journal logging integration

**Service Management:**

```bash
# Uninstall
sudo muti-metroo uninstall --name muti-metroo

# View status
systemctl status muti-metroo

# View logs
journalctl -u muti-metroo -f
```

**macOS Installation:**

```bash
# Via setup wizard
sudo muti-metroo setup

# Manual installation handled by wizard
# Creates /Library/LaunchDaemons/com.muti-metroo.plist
```

**macOS Service Management:**

```bash
# Load/start service
sudo launchctl load -w /Library/LaunchDaemons/com.muti-metroo.plist

# Unload/stop service
sudo launchctl unload -w /Library/LaunchDaemons/com.muti-metroo.plist

# View logs
tail -f /path/to/working/dir/muti-metroo.log
```

### 16.2 Setup Wizard

Interactive CLI wizard for initial configuration:

```bash
muti-metroo setup
```

**Wizard Steps:**

1. Basic setup (data directory, config path)
2. Agent role selection (ingress, transit, exit)
3. Network configuration (transport, address)
4. TLS setup (generate, paste, or use existing certs)
5. Peer connections
6. SOCKS5 configuration (if ingress)
7. Exit configuration (if exit)
8. Advanced options (logging, health, control)
9. Service installation (on supported platforms)

---

## 17. Certificate Management

### 17.1 Default Behavior

By default, TLS certificates are auto-generated and verification is disabled:

- Agent generates self-signed ECDSA (P-256) certificate on startup
- Certificates are ephemeral (regenerated each startup)
- No CA required, no certificate verification
- Safe because E2E encryption (X25519 + ChaCha20-Poly1305) protects payload

**Config for strict TLS:**

```yaml
tls:
  ca: "./certs/ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  strict: true # Enable certificate verification
  mtls: true # Optional: require client certificates
```

### 17.2 Certificate Types

| Type       | Usage                                     | Default Validity |
| ---------- | ----------------------------------------- | ---------------- |
| CA         | Sign other certificates                   | 365 days         |
| Agent/Peer | Server + client auth for mesh connections | 90 days          |
| Client     | Client authentication only                | 90 days          |

### 17.3 Certificate Generation

```bash
# Generate CA
muti-metroo cert ca \
  --cn "My CA" \
  --out ./certs \
  --days 365

# Generate agent certificate (signed by CA)
muti-metroo cert agent \
  --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  --dns "agent-1.local,localhost" \
  --ip "127.0.0.1,192.168.1.100"

# Generate client certificate
muti-metroo cert client \
  --cn "client-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key

# View certificate details
muti-metroo cert info ./certs/agent-1.crt
```

### 17.4 Certificate Details

**Algorithm:** ECDSA P-256

**Output Files:**

- `{name}.crt` - PEM-encoded certificate (mode 0644)
- `{name}.key` - PEM-encoded private key (mode 0600)

**Fingerprint Format:** `sha256:<hex>`

**Key Usages:**

- CA: Certificate signing, CRL signing
- Server: Digital signature, key encipherment, server auth
- Client: Digital signature, client auth
- Peer: Digital signature, key encipherment, server auth, client auth

---

## 18. Project Structure

### 18.1 Directory Layout

```
muti-metroo/
├── cmd/
│   ├── muti-metroo/
│   │   └── main.go                 # CLI entrypoint and all commands
│   └── muti-dll/
│       └── main.go                 # Windows DLL entry point for service installation
│
├── internal/
│   ├── agent/
│   │   ├── agent.go                # Main agent orchestration
│   │   ├── relay_table.go          # Shared relay table for TCP/UDP/ICMP transit
│   │   ├── udp.go                  # UDP relay integration
│   │   ├── icmp.go                 # ICMP echo integration
│   │   └── agent_test.go           # Agent tests
│   │
│   ├── config/
│   │   ├── config.go               # Configuration parsing and validation
│   │   └── config_test.go          # Configuration tests
│   │
│   ├── dynconfig/
│   │   ├── dynconfig.go            # Append-only JSONL journal for runtime peer/forward/route/display-name mutations
│   │   └── dynconfig_test.go       # Journal tests
│   │
│   ├── identity/
│   │   ├── identity.go             # AgentID generation/storage
│   │   ├── keypair.go              # X25519 keypair for E2E encryption
│   │   ├── identity_test.go        # Identity tests
│   │   └── keypair_test.go         # Keypair tests
│   │
│   ├── crypto/
│   │   ├── crypto.go               # E2E encryption: X25519 + ChaCha20-Poly1305
│   │   ├── sealed.go               # Sealed box for management key encryption
│   │   ├── signing.go              # Ed25519 signing for sleep/wake commands
│   │   ├── crypto_test.go          # Crypto tests
│   │   ├── sealed_test.go          # Sealed box tests
│   │   └── signing_test.go         # Signing tests
│   │
│   ├── transport/
│   │   ├── transport.go            # Transport interface
│   │   ├── quic.go                 # QUIC implementation
│   │   ├── h2.go                   # HTTP/2 implementation
│   │   ├── ws.go                   # WebSocket implementation
│   │   ├── tls.go                  # TLS helpers
│   │   ├── fingerprint.go          # TLS ClientHello profiles (uTLS)
│   │   ├── transport_test.go       # Transport tests
│   │   ├── h2_test.go              # HTTP/2 tests
│   │   └── ws_test.go              # WebSocket tests
│   │
│   ├── peer/
│   │   ├── manager.go              # Peer lifecycle management
│   │   ├── connection.go           # Single peer connection
│   │   ├── handshake.go            # PEER_HELLO handling
│   │   ├── reconnect.go            # Reconnection logic
│   │   ├── peer_test.go            # Peer tests
│   │   └── handshake_test.go       # Handshake tests
│   │
│   ├── protocol/
│   │   ├── frame.go                # Frame encode/decode
│   │   ├── types.go                # Frame types, flags, error codes
│   │   ├── protocol_test.go        # Protocol tests
│   │   ├── udp_test.go             # UDP frame tests
│   │   └── icmp_test.go            # ICMP frame tests
│   │
│   ├── stream/
│   │   ├── manager.go              # Stream lifecycle and forward table
│   │   └── stream_test.go          # Stream tests
│   │
│   ├── routing/
│   │   ├── table.go                # CIDR route table with longest-prefix match
│   │   ├── domain.go               # Domain route table with exact/wildcard matching
│   │   ├── forward.go              # Forward route table for port forwarding keys
│   │   ├── agent.go                # Agent presence table
│   │   ├── manager.go              # Route management (dynamic routes)
│   │   ├── routing_test.go         # CIDR routing tests
│   │   ├── domain_test.go          # Domain routing tests
│   │   └── agent_test.go           # Agent presence tests
│   │
│   ├── flood/
│   │   ├── flood.go                # Flood protocol (advertise/withdraw)
│   │   └── flood_test.go           # Flood tests
│   │
│   ├── forward/
│   │   ├── forward.go              # Endpoint struct, ForwardDialer interface
│   │   ├── handler.go              # Exit point handler for port forwarding
│   │   ├── listener.go             # TCP listener for incoming connections
│   │   ├── forward_test.go         # Forward unit tests
│   │   ├── handler_test.go         # Handler unit tests
│   │   └── listener_test.go        # Listener unit tests
│   │
│   ├── socks5/
│   │   ├── server.go               # SOCKS5 server
│   │   ├── handler.go              # SOCKS5 command handler
│   │   ├── socks4.go               # SOCKS4/4a request handling
│   │   ├── auth.go                 # Authentication
│   │   ├── udp.go                  # UDP ASSOCIATE handler
│   │   ├── icmp.go                 # ICMP ping integration (CmdICMPEcho 0x04)
│   │   ├── routewatch.go           # Route-watch/snapshot (CmdRouteWatch 0x05 / CmdSnapshot 0x06)
│   │   ├── expose.go               # Expose-over-SOCKS5 dispatch (CmdExpose 0x07)
│   │   ├── ws_listener.go          # WebSocket SOCKS5 listener
│   │   ├── conn_tracker.go         # Generic connection tracker
│   │   ├── socks5_test.go          # SOCKS5 tests
│   │   ├── udp_test.go             # UDP tests
│   │   ├── ws_listener_test.go     # WebSocket listener tests
│   │   └── auth_security_test.go   # Auth security tests
│   │
│   ├── expose/                     # Ephemeral forward endpoints over SOCKS5 CmdExpose
│   │   ├── manager.go              # Session manager (HandleRawConn)
│   │   ├── session.go              # Per-connection session + muxed streams
│   │   ├── transport.go            # messageConn seam + length-prefixed rawConn
│   │   ├── protocol.go             # Expose frame encode/decode
│   │   └── backend.go              # forward.Backend adapter
│   │
│   ├── exit/
│   │   ├── handler.go              # Exit handler
│   │   ├── dns.go                  # DNS resolution
│   │   └── exit_test.go            # Exit tests
│   │
│   ├── embed/
│   │   ├── embed.go                # Binary config embedding (XOR obfuscation)
│   │   └── embed_test.go           # Embed tests
│   │
│   ├── certutil/
│   │   ├── certutil.go             # Certificate generation and management
│   │   └── certutil_test.go        # Certificate tests
│   │
│   ├── health/
│   │   ├── server.go               # Health check HTTP server and API endpoints
│   │   ├── shell.go                # WebSocket shell relay handler
│   │   ├── icmp.go                 # WebSocket ICMP relay handler
│   │   ├── meshtest.go             # Mesh connectivity test handler
│   │   ├── logo.go                 # Embedded logo for splash page
│   │   └── server_test.go          # Health server tests
│   │
│   ├── udp/
│   │   ├── handler.go              # UDP relay handler (SOCKS5 UDP ASSOCIATE)
│   │   ├── association.go          # UDP association lifecycle management
│   │   ├── config.go               # UDP configuration
│   │   ├── doc.go                  # Package documentation
│   │   ├── handler_test.go         # Handler tests
│   │   ├── association_test.go     # Association tests
│   │   └── config_test.go          # Config tests
│   │
│   ├── icmp/
│   │   ├── handler.go              # ICMP echo (ping) handler at exit node
│   │   ├── socket.go               # Platform-specific ICMP socket operations
│   │   ├── session.go              # ICMP session state management
│   │   ├── config.go               # ICMP CIDR validation and configuration
│   │   ├── doc.go                  # Package documentation
│   │   ├── handler_test.go         # Handler tests
│   │   ├── config_test.go          # Config tests
│   │   ├── session_test.go         # Session tests
│   │   └── socket_test.go          # Socket tests
│   │
│   ├── probe/
│   │   ├── probe.go                # Connectivity testing for listeners
│   │   ├── listen.go               # Probe listener for verifying inbound connectivity
│   │   └── listen_test.go          # Probe listener tests
│   │
│   ├── wizard/
│   │   ├── wizard.go               # Setup wizard implementation
│   │   ├── wizard_test.go          # Wizard tests
│   │   └── prompt/
│   │       └── prompt.go           # Terminal prompt utilities for wizard
│   │
│   ├── service/
│   │   ├── service.go              # Service management interface and DeriveNames
│   │   ├── service_linux.go        # Linux systemd implementation
│   │   ├── service_darwin.go       # macOS launchd implementation
│   │   ├── service_windows.go      # Windows service implementation
│   │   ├── service_other.go        # Stub for unsupported platforms
│   │   ├── service_test.go         # Service tests
│   │   ├── service_linux_test.go   # Linux-specific tests
│   │   └── service_windows_test.go # Windows-specific tests
│   │
│   ├── shell/
│   │   ├── handler.go              # Shell request/response handling
│   │   ├── executor.go             # Command execution (PTY and streaming)
│   │   ├── client.go               # Shell client for CLI
│   │   ├── messages.go             # Wire protocol messages
│   │   ├── pty_unix.go             # PTY allocation for Unix platforms
│   │   ├── pty_windows.go          # ConPTY for Windows
│   │   ├── signal_unix.go          # Signal handling for Unix
│   │   ├── signal_windows.go       # Signal handling for Windows
│   │   ├── handler_test.go         # Handler tests
│   │   ├── executor_test.go        # Executor tests
│   │   ├── client_test.go          # Client tests
│   │   └── messages_test.go        # Messages tests
│   │
│   ├── sleep/
│   │   ├── sleep.go                # Sleep manager state machine, persistence
│   │   ├── queue.go                # State queue for sleeping peers
│   │   ├── window.go               # Deterministic listening window calculator
│   │   ├── sleep_test.go           # Sleep tests
│   │   └── window_test.go          # Window calculator tests
│   │
│   ├── filetransfer/
│   │   ├── stream.go               # Stream-based file transfer protocol
│   │   ├── tar.go                  # Directory tar/untar with gzip compression
│   │   ├── browse.go               # File browsing (directory listing, stat, roots)
│   │   ├── browse_drives_other.go  # Unix wildcard root "/" (build !windows)
│   │   ├── browse_drives_windows.go # Windows logical drive enumeration
│   │   ├── partial.go              # Partial/resumable transfers
│   │   ├── ratelimit.go            # Bandwidth rate limiting
│   │   ├── size.go                 # Human-readable size formatting
│   │   ├── stream_test.go          # Stream transfer tests
│   │   ├── tar_test.go             # Tar archive tests
│   │   ├── browse_test.go          # Browse tests
│   │   ├── browse_drives_windows_test.go # Windows drive enumeration tests
│   │   ├── partial_test.go         # Partial transfer tests
│   │   ├── ratelimit_test.go       # Rate limit tests
│   │   ├── security_test.go        # Security tests
│   │   └── size_test.go            # Size tests
│   │
│   ├── hosts/
│   │   ├── store.go                # Mesh-host registry (config + journalled runtime entries, suffixes)
│   │   └── store_test.go           # Host registry tests
│   │
│   ├── apiclient/
│   │   └── client.go               # HTTP client used by the CLI (token auth, signed envelopes)
│   │
│   ├── sysinfo/
│   │   ├── sysinfo.go              # System info and shell detection for node advertisements
│   │   └── sysinfo_test.go         # System info tests
│   │
│   ├── logging/
│   │   ├── logging.go              # Structured logging utilities
│   │   └── logging_test.go         # Logging tests
│   │
│   ├── recovery/
│   │   ├── recovery.go             # Panic recovery utilities
│   │   └── recovery_test.go        # Recovery tests
│   │
│   ├── chaos/
│   │   ├── chaos.go                # Fault injection for testing
│   │   ├── chaos_test.go           # Chaos testing tests
│   │   └── connection_state_test.go # Connection state tests
│   │
│   ├── loadtest/
│   │   ├── loadtest.go             # Load testing utilities
│   │   └── loadtest_test.go        # Load test tests
│   │
│   └── integration/
│       ├── README.md               # Integration test conventions
│       ├── coverage.csv            # Coverage matrix per feature
│       ├── agent_chain_test.go     # Agent chain orchestration tests
│       ├── chain_test.go           # Multi-agent chain tests
│       ├── dns_responder_test.go   # Local DNS responder utilities
│       ├── e2e_stream_test.go      # End-to-end stream tests
│       ├── exit_cidr_test.go       # Exit CIDR filtering tests
│       ├── exit_domain_test.go     # Domain-based exit routing tests
│       ├── file_directory_test.go  # Directory transfer tests
│       ├── file_transfer_test.go   # File transfer integration tests
│       ├── forward_test.go         # Port forwarding integration tests
│       ├── halfclose_test.go       # Half-close semantics tests
│       ├── http_auth_test.go       # HTTP API bearer auth tests
│       ├── icmp_test.go            # ICMP echo integration tests
│       ├── mesh_runner_test.go     # Mesh runner test utilities
│       ├── multi_transport_ssh_test.go # Multi-transport SSH tests
│       ├── reconnect_test.go       # Reconnection tests
│       ├── shell_test.go           # Management command integration tests
│       ├── sleep_test.go           # Sleep mode integration tests
│       ├── socks5_auth_test.go     # SOCKS5 authentication tests
│       ├── transport_relay_test.go # Transport relay tests
│       └── udp_relay_test.go       # UDP relay integration tests
│
├── configs/
│   └── example.yaml                # Example configuration
│
├── docs/                           # Docusaurus documentation site
│
├── user-manual/                    # Standalone PDF user manual
│
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── Dockerfile.test
├── docker-compose.yml              # Multi-agent test environment
├── docker-compose.override.yml     # Local overrides
└── README.md
```

### 18.2 Dependencies

| Package                                 | Purpose                                                   |
| --------------------------------------- | --------------------------------------------------------- |
| `github.com/quic-go/quic-go`            | QUIC transport                                            |
| `golang.org/x/net/http2`                | HTTP/2 transport                                          |
| `nhooyr.io/websocket`                   | WebSocket transport (SOCKS5 WS)                           |
| `github.com/gorilla/websocket`          | WebSocket (shell, ICMP relay)                             |
| `github.com/refraction-networking/utls` | TLS ClientHello profiles (browser-compatible ClientHello) |
| `golang.org/x/crypto`                   | bcrypt, Ed25519, X25519                                   |
| `gopkg.in/yaml.v3`                      | Configuration parsing                                     |
| `github.com/spf13/cobra`                | CLI framework                                             |
| `log/slog`                              | Structured logging (stdlib)                               |
| `github.com/charmbracelet/x/conpty`     | Windows ConPTY for interactive shell                      |

---

## 19. Implementation Notes

### 19.1 Critical Settings

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        CRITICAL SETTINGS                                    │
│                                                                             │
│  TCP_NODELAY (Nagle's algorithm):                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  MUST disable Nagle's algorithm on all TCP connections.             │    │
│  │  Without this, small packets (SSH keystrokes) are delayed.          │    │
│  │                                                                     │    │
│  │  Go: conn.(*net.TCPConn).SetNoDelay(true)                           │    │
│  │                                                                     │    │
│  │  Apply to:                                                          │    │
│  │  • HTTP/2 transport connections                                     │    │
│  │  • WebSocket transport connections                                  │    │
│  │  • Exit handler TCP connections                                     │    │
│  │  • SOCKS5 client connections                                        │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Buffer sizes:                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Default: 256 KB per stream                                         │    │
│  │  Suitable for video streaming                                       │    │
│  │  Configurable for memory-constrained environments                   │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Frame size:                                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Maximum: 16 KB payload                                             │    │
│  │  Ensures fairness between streams                                   │    │
│  │  Prevents one stream from blocking others                           │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 19.2 Goroutine Management

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                       GOROUTINE MANAGEMENT                                  │
│                                                                             │
│  Per peer connection:                                                       │
│  • 1 reader goroutine (decodes frames, dispatches to ordered or fast-lane   │
│    channel)                                                                 │
│  • 1 keepalive goroutine (periodic probes on stream 0)                      │
│  • 1 ordered-frame dispatch goroutine (drains frameCh)                      │
│  • 4 fast-lane dispatch goroutines (drain fastLaneCh for UDP_DATAGRAM /     │
│    ICMP_ECHO frames; count = fastLaneWorkerCount)                           │
│                                                                             │
│  Writes are serialised via a mutex (writeMu) around synchronous WriteFrame  │
│  calls; there is no dedicated writer goroutine. Each WriteFrame arms a      │
│  per-write deadline (connections.write_timeout, default 30s) on the         │
│  control stream so a peer that stops reading fails the write -- and tears   │
│  down the connection -- instead of holding writeMu forever.                 │
│                                                                             │
│  Per stream (at endpoints):                                                 │
│  • 2 goroutines for bidirectional relay                                     │
│                                                                             │
│  Leak prevention:                                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  • Use context.Context for cancellation                             │    │
│  │  • Set read/write deadlines                                         │    │
│  │  • Close underlying connections on shutdown                         │    │
│  │  • Use recovery.RecoverWithLog for panic handling                   │    │
│  │  • Log goroutine count periodically (debug mode)                    │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 19.3 Error Handling

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         ERROR HANDLING                                      │
│                                                                             │
│  Connection errors:                                                         │
│  • Log error with peer ID                                                   │
│  • Clean up streams (send STREAM_RESET)                                     │
│  • Remove routes via this peer                                              │
│  • Trigger reconnection                                                     │
│                                                                             │
│  Stream errors:                                                             │
│  • Log error with stream ID                                                 │
│  • Send STREAM_RESET to both ends                                           │
│  • Remove from forward table                                                │
│  • Close local connections if any                                           │
│                                                                             │
│  Flood propagation errors:                                                  │
│  • Log at debug level (non-fatal)                                           │
│  • Continue sending to other peers                                          │
│  • Do not fail the entire flood operation                                   │
│                                                                             │
│  Configuration errors:                                                      │
│  • Validate on startup                                                      │
│  • Fail fast with clear error message                                       │
│                                                                             │
│  Resource exhaustion:                                                       │
│  • Reject new streams with RESOURCE_LIMIT error                             │
│  • Log warning                                                              │
│  • Don't kill existing streams                                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 20. Testing Strategy

### 20.1 Test Levels

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          TEST LEVELS                                        │
│                                                                             │
│  Unit Tests:                                                                │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  • Frame encode/decode                                              │    │
│  │  • Route table operations (insert, lookup, expire)                  │    │
│  │  • Stream state machine transitions                                 │    │
│  │  • CIDR matching                                                    │    │
│  │  • Configuration parsing and validation                             │    │
│  │  • Certificate generation                                           │    │
│  │  • Wizard config building                                           │    │
│  │  • Service unit generation                                          │    │
│  │  • Handshake failure scenarios                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  Integration Tests:                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  • 2-agent chain (ingress → exit)                                   │    │
│  │  • 3-agent chain (ingress → transit → exit)                         │    │
│  │  • Each transport type (QUIC, HTTP/2, WebSocket)                    │    │
│  │  • Reconnection behavior                                            │    │
│  │  • Route propagation                                                │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  End-to-End Tests:                                                          │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  • SSH session through 3-agent chain                                │    │
│  │  • HTTP request/response                                            │    │
│  │  • Large file transfer                                              │    │
│  │  • Concurrent streams (SSH + video simulation)                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 20.2 Running Tests

```bash
# Run all tests with race detection
make test

# Run with coverage report
make test-coverage

# Run specific package tests
go test -v ./internal/config/...

# Run single test
go test -v -run TestHandshake_Success ./internal/peer/

# Run tests in Docker
docker build -f Dockerfile.test -t muti-metroo-test .
docker run --rm muti-metroo-test
```

---

## Appendix A: Quick Reference

### Frame Types

| Code | Name                | Description                         |
| ---- | ------------------- | ----------------------------------- |
| 0x01 | STREAM_OPEN         | Open virtual stream                 |
| 0x02 | STREAM_OPEN_ACK     | Stream opened                       |
| 0x03 | STREAM_OPEN_ERR     | Stream failed                       |
| 0x04 | STREAM_DATA         | Payload data                        |
| 0x05 | STREAM_CLOSE        | Graceful close                      |
| 0x06 | STREAM_RESET        | Abort stream                        |
| 0x10 | ROUTE_ADVERTISE     | Announce CIDR/domain/forward routes |
| 0x11 | ROUTE_WITHDRAW      | Remove CIDR/domain/forward routes   |
| 0x12 | NODE_INFO_ADVERTISE | Announce node metadata              |
| 0x20 | PEER_HELLO          | Handshake                           |
| 0x21 | PEER_HELLO_ACK      | Handshake response                  |
| 0x22 | KEEPALIVE           | Liveness probe                      |
| 0x23 | KEEPALIVE_ACK       | Liveness response                   |
| 0x24 | CONTROL_REQUEST     | Request status/RPC                  |
| 0x25 | CONTROL_RESPONSE    | Response with data                  |
| 0x30 | UDP_OPEN            | Request UDP association             |
| 0x31 | UDP_OPEN_ACK        | Association established             |
| 0x32 | UDP_OPEN_ERR        | Association failed                  |
| 0x33 | UDP_DATAGRAM        | UDP datagram payload                |
| 0x34 | UDP_CLOSE           | Close association                   |
| 0x40 | ICMP_OPEN           | Request ICMP session                |
| 0x41 | ICMP_OPEN_ACK       | Session established                 |
| 0x42 | ICMP_OPEN_ERR       | Session failed                      |
| 0x43 | ICMP_ECHO           | Echo request/reply                  |
| 0x44 | ICMP_CLOSE          | Close session                       |
| 0x50 | SLEEP_COMMAND       | Mesh-wide sleep                     |
| 0x51 | WAKE_COMMAND        | Mesh-wide wake                      |
| 0x52 | QUEUED_STATE        | Queued state for peer               |

### Error Codes

| Code | Name                  | Description                           |
| ---- | --------------------- | ------------------------------------- |
| 1    | NO_ROUTE              | No route to destination               |
| 2    | CONNECTION_REFUSED    | Target refused connection             |
| 3    | CONNECTION_TIMEOUT    | Connection attempt timed out          |
| 4    | TTL_EXCEEDED          | TTL reached zero                      |
| 5    | HOST_UNREACHABLE      | Cannot reach target host              |
| 6    | NETWORK_UNREACHABLE   | Cannot reach target network           |
| 7    | DNS_ERROR             | Domain name resolution failed         |
| 8    | EXIT_DISABLED         | Exit functionality not enabled        |
| 9    | RESOURCE_LIMIT        | Too many streams                      |
| 10   | CONNECTION_LIMIT      | Connection limit exceeded             |
| 11   | NOT_ALLOWED           | Operation not permitted               |
| 12   | FILE_TRANSFER_DENIED  | File transfer not allowed             |
| 13   | AUTH_REQUIRED         | Authentication required               |
| 14   | PATH_NOT_ALLOWED      | Path not in allowed list              |
| 15   | FILE_TOO_LARGE        | File exceeds size limit               |
| 16   | FILE_NOT_FOUND        | File does not exist                   |
| 17   | WRITE_FAILED          | Write operation failed                |
| 18   | GENERAL_FAILURE       | General error                         |
| 19   | RESUME_FAILED         | Resume not possible                   |
| 20   | SHELL_DISABLED        | Shell feature is disabled             |
| 21   | SHELL_AUTH_FAILED     | Shell authentication failed           |
| 22   | PTY_FAILED            | PTY allocation failed                 |
| 23   | COMMAND_NOT_ALLOWED   | Command not in whitelist              |
| 30   | UDP_DISABLED          | UDP relay is disabled                 |
| 31   | UDP_PORT_NOT_ALLOWED  | UDP port not in whitelist             |
| 40   | FORWARD_NOT_FOUND     | Port forward key not configured       |
| 50   | ICMP_DISABLED         | ICMP feature is disabled              |
| 51   | ICMP_DEST_NOT_ALLOWED | ICMP destination not in allowed CIDRs |
| 52   | ICMP_SESSION_LIMIT    | Max ICMP sessions reached             |

### Default Timing

| Parameter                   | Default                                          |
| --------------------------- | ------------------------------------------------ |
| Keepalive idle threshold    | 5m                                               |
| Keepalive timeout           | 90s                                              |
| Keepalive jitter            | 0.2                                              |
| Route TTL                   | 5m                                               |
| Advertise interval          | 2m                                               |
| Node info interval          | 2m (falls back to advertise interval when unset) |
| Reconnect initial delay     | 1s                                               |
| Reconnect max delay         | 60s                                              |
| Reconnect multiplier        | 2.0                                              |
| Reconnect jitter            | 0.2                                              |
| Stream open timeout         | 30s                                              |
| Handshake timeout           | 10s                                              |
| QUIC idle timeout           | 60s                                              |
| QUIC keepalive period       | 30s                                              |
| UDP idle timeout            | 5m                                               |
| ICMP session idle timeout   | 60s                                              |
| ICMP per-echo timeout       | 5s                                               |
| Sleep poll interval         | 5m                                               |
| Sleep poll jitter           | 0.3                                              |
| Sleep poll duration         | 30s                                              |
| Sleep command replay window | 5m                                               |

### CLI Commands

| Command                   | Description                                     |
| ------------------------- | ----------------------------------------------- |
| `setup`                   | Interactive configuration wizard                |
| `init`                    | Initialize agent identity                       |
| `run`                     | Start the agent                                 |
| `status`                  | Show agent status                               |
| `peers`                   | List connected peers                            |
| `routes`                  | Show routing table                              |
| `probe`                   | Test connectivity to a listener                 |
| `mesh-test`               | Test connectivity to all mesh agents            |
| `ping`                    | ICMP ping through remote exit agent             |
| `shell`                   | Execute command on remote agent                 |
| `upload`                  | Upload file to remote agent                     |
| `download`                | Download file from remote agent                 |
| `route add`               | Add dynamic CIDR exit route                     |
| `route remove`            | Remove dynamic CIDR exit route                  |
| `route list`              | List dynamic routes                             |
| `forward add`             | Add dynamic forward listener                    |
| `forward remove`          | Remove dynamic forward listener                 |
| `forward list`            | List forward listeners                          |
| `peer add`                | Add dynamic outbound peer connection            |
| `peer remove`             | Remove dynamic peer connection                  |
| `peer list`               | List peer connections (config + dynamic)        |
| `display-name set`        | Set agent display name                          |
| `display-name get`        | Get current display name                        |
| `cert ca`                 | Generate CA certificate                         |
| `cert agent`              | Generate agent certificate                      |
| `cert client`             | Generate client certificate                     |
| `cert info`               | Display certificate details                     |
| `hash`                    | Generate bcrypt password hash                   |
| `management-key generate` | Generate X25519 keypair for topology encryption |
| `management-key public`   | Derive public key from private key              |
| `signing-key generate`    | Generate Ed25519 keypair for sleep/wake auth    |
| `signing-key public`      | Derive public key from private key              |
| `service install`         | Install as system service                       |
| `service uninstall`       | Remove system service                           |
| `service status`          | Check service status                            |
| `sleep`                   | Put mesh into sleep mode                        |
| `wake`                    | Wake mesh from sleep mode                       |
| `sleep-status`            | Check sleep mode status                         |

---

## Appendix B: Mutiauk - Optional TUN Interface

Mutiauk is an optional companion tool that provides transparent Layer 3 traffic interception, forwarding traffic through Muti Metroo's SOCKS5 proxy.

### Overview

While Muti Metroo requires applications to be SOCKS5-aware, Mutiauk enables any application to use the mesh network without modification by:

1. Creating a TUN network interface
2. Intercepting L3 (IP) traffic destined for configured routes
3. Forwarding TCP and UDP connections through SOCKS5
4. Returning responses back through the TUN interface

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         MUTI_METROO ENTRY DATA FLOW                               │
│                                                                             │
│  Application → TUN Device → gVisor Stack → TCP/UDP Forwarder → SOCKS5 →     │
│                                                               Muti Metroo →       │
│                                                               Exit Agent →  │
│                                                               Destination   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Platform Requirements

| Aspect           | Requirement                   |
| ---------------- | ----------------------------- |
| **Platform**     | Linux only (TUN interface)    |
| **Privileges**   | Root required (CAP_NET_ADMIN) |
| **Dependencies** | None (static binary)          |

**Note:** Unlike Muti Metroo agents which run unprivileged, Mutiauk requires root to create and manage the TUN interface.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         MUTI_METROO ENTRY ARCHITECTURE                            │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                          DAEMON                                       │  │
│  │                                                                       │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐           │  │
│  │  │  TUN Device    │  │  gVisor Stack  │  │  NAT Table     │           │  │
│  │  │                │  │                │  │                │           │  │
│  │  │ • /dev/net/tun │  │ • TCP forwarder│  │ • Connection   │           │  │
│  │  │ • TUNSETIFF    │  │ • UDP forwarder│  │   tracking     │           │  │
│  │  │ • IP packets   │  │ • gonet        │  │ • TTL expiry   │           │  │
│  │  └────────────────┘  └────────────────┘  └────────────────┘           │  │
│  │                                                                       │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐           │  │
│  │  │  Route Manager │  │  SOCKS5 Client │  │  Config Watch  │           │  │
│  │  │                │  │                │  │                │           │  │
│  │  │ • netlink      │  │ • CONNECT      │  │ • fsnotify     │           │  │
│  │  │ • CIDR routes  │  │ • UDP ASSOCIATE│  │ • hot reload   │           │  │
│  │  │ • conflict det │  │ • auth         │  │ • SIGHUP       │           │  │
│  │  └────────────────┘  └────────────────┘  └────────────────┘           │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Package Structure

```
mutiauk/
├── cmd/mutiauk/
│   └── main.go              # CLI entrypoint
├── internal/
│   ├── daemon/              # Orchestrator (server.go)
│   ├── tun/                 # TUN device creation
│   ├── stack/               # gVisor TCP/IP stack wrapper
│   ├── proxy/               # TCP/UDP forwarders
│   ├── socks5/              # SOCKS5 client
│   ├── nat/                 # Connection tracking
│   ├── route/               # Linux route management
│   ├── config/              # YAML configuration
│   ├── cli/                 # CLI commands
│   ├── wizard/              # Interactive setup wizard
│   └── service/             # Systemd service management
└── configs/
    └── mutiauk.example.yaml
```

### CLI Commands

| Command             | Description                                                              |
| ------------------- | ------------------------------------------------------------------------ |
| `setup`             | Interactive configuration wizard                                         |
| `daemon start`      | Start the daemon                                                         |
| `daemon stop`       | Stop the daemon                                                          |
| `daemon reload`     | Reload configuration                                                     |
| `route list`        | List active routes                                                       |
| `route add`         | Add a route                                                              |
| `route remove`      | Remove a route                                                           |
| `status`            | Show comprehensive daemon status (top-level command, not under `daemon`) |
| `service install`   | Install systemd service                                                  |
| `service uninstall` | Remove systemd service                                                   |

### Configuration

```yaml
# /etc/mutiauk/config.yaml
daemon:
  pid_file: /var/run/mutiauk.pid
  socket_path: /var/run/mutiauk.sock

tun:
  name: tun0
  mtu: 1400
  address: 10.200.200.1/24

socks5:
  server: 127.0.0.1:1080 # Muti Metroo SOCKS5 address
  username: ""
  password: ""
  timeout: 30s

routes:
  - destination: 10.0.0.0/8
    comment: "Internal network"
    enabled: true

nat:
  table_size: 65536
  tcp_timeout: 1h
  udp_timeout: 5m
```

### Relationship with Muti Metroo

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     MUTI_METROO ENTRY + MUTI_METROO DEPLOYMENT                          │
│                                                                             │
│  ┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐     │
│  │   Application    │     │     Mutiauk  │     │   Muti Metroo          │     │
│  │   (any app)      │     │  (Linux, root)   │     │   (userspace)    │     │
│  │                  │     │                  │     │                  │     │
│  │  No SOCKS5       │     │  TUN interface   │     │  SOCKS5 ingress  │     │
│  │  configuration   │────►│  IP → SOCKS5     │────►│  Mesh routing    │     │
│  │  required        │     │  TCP + UDP       │     │  E2E encryption  │     │
│  │                  │     │                  │     │                  │     │
│  └──────────────────┘     └──────────────────┘     └──────────────────┘     │
│                                                              │              │
│                                                              ▼              │
│                                                    ┌──────────────────┐     │
│                                                    │   Exit Agent     │     │
│                                                    │                  │     │
│                                                    │  Real TCP/UDP    │     │
│                                                    │  connections     │     │
│                                                    └──────────────────┘     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Documentation

Mutiauk source code: https://github.com/postalsys/Mutiauk

Source code location: `../Mutiauk/` (sibling directory to Muti Metroo)

---

_End of Document_
