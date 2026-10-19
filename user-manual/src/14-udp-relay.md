# UDP Relay

UDP relay lets the SOCKS5 listener carry UDP datagrams through the mesh, with the standard UDP ASSOCIATE command (RFC 1928) or with UDP STREAM, a Muti Metroo extension that carries the datagrams on the SOCKS connection itself. Mutiauk uses UDP STREAM. The ingress agent forwards each datagram through the mesh to the exit agent that has a route for its destination, and the exit sends it from its own host. This is primarily used for DNS and NTP traffic.

Datagram payloads are end-to-end encrypted between the ingress and the exit. Destination addresses are visible to transit agents.

## Requirements

- The ingress agent has the SOCKS5 listener enabled (Chapter 9).
- The exit agent has UDP relay enabled. It is enabled by default.
- The ingress agent has a CIDR route for each destination. Unlike TCP, UDP is never sent directly from the ingress host when no route matches.
- The route's exit is a different agent from the ingress. An agent does not relay UDP to its own exit routes, so a single agent that is both ingress and exit does not relay UDP.
- The ingress agent has a route that covers `0.0.0.0` (in practice, a `0.0.0.0/0` default route somewhere in the mesh). UDP ASSOCIATE and UDP STREAM are refused without one, even if every destination you plan to use has a more specific route.

The UDP ASSOCIATE relay socket listens on the same IP address as the SOCKS5 TCP listener, so a listener on `127.0.0.1:1080` only accepts datagrams from the local host. The relay is IPv4.

## Configuration

Configure on the **exit** agent:

```yaml
udp:
  enabled: true                # Enabled by default
  max_associations: 1000       # Max concurrent associations
  idle_timeout: 5m             # Association timeout
  max_datagram_size: 1472      # Max UDP payload
```

## Usage

With UDP ASSOCIATE, the client opens a TCP control connection to the ingress agent, receives a local UDP relay address in the reply, and sends its datagrams there. Mutiauk relays UDP for all programs on the operator workstation through its TUN interface, using UDP STREAM (below) and falling back to UDP ASSOCIATE, so UDP applications need no extra setup.

A direct client must implement SOCKS5 UDP ASSOCIATE or UDP STREAM. Many SOCKS tools, including proxychains and curl, only proxy TCP. The example below tests UDP relay with a direct SOCKS client.

### Testing with socksify

```bash
socksify dig @8.8.8.8 example.com
```

## UDP over the SOCKS Connection (UDP STREAM)

UDP STREAM (SOCKS5 command `0x08`) is UDP ASSOCIATE without the separate relay socket. After the success reply, each datagram travels on the SOCKS connection in both directions as a 2-byte big-endian length followed by the same SOCKS5 UDP header (RFC 1928) and payload that UDP ASSOCIATE uses. Compared with UDP ASSOCIATE:

- It works over the SOCKS5 WebSocket listener (Chapter 9), and with TLS (`wss://`) the datagrams are encrypted between the client and the ingress agent. The UDP ASSOCIATE relay socket is plain UDP, and only reachable where the client can send UDP to the ingress host.
- No UDP port on the ingress host needs to be reachable from the client.
- Datagrams share the connection's TCP ordering, so a lost packet on that connection delays the datagrams behind it.

An agent that does not support UDP STREAM, or has UDP relay disabled, replies with "command not supported" (reply code 7). Mutiauk then falls back to UDP ASSOCIATE. The limits below apply to both commands, with the connection taking the place of the control connection.

## Full Example

### Exit Agent Configuration

```yaml
agent:
  display_name: "Exit with UDP"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

exit:
  enabled: true
  routes:
    - "0.0.0.0/0"
  # dns.servers defaults to [] -> use the host's system resolver.
  # UDP relay itself does not require explicit DNS servers; configure
  # only when overriding for split-horizon, internal-resolver, or
  # geofencing reasons.

udp:
  enabled: true
  max_associations: 1000
  idle_timeout: 5m
```

### Ingress Agent Configuration

```yaml
agent:
  display_name: "Ingress"

peers:
  - id: "exit-agent-id..."
    transport: quic
    address: "exit.example.com:4433"

socks5:
  enabled: true
  address: "127.0.0.1:1080"
```

## Limitations

- **Maximum datagram size**: 1472 bytes
- **No fragmentation**: Datagrams with frag > 0 are rejected
- **Association lifetime**: Tied to the TCP control connection (UDP ASSOCIATE) or the SOCKS connection (UDP STREAM)
- **No same-agent relay**: An agent does not relay UDP to its own exit routes; the route's exit must be another agent

## Security Considerations

1. **Limit associations**: Set reasonable `max_associations`
2. **Monitor usage**: UDP can be used for tunneling
3. **Timeouts**: Use appropriate `idle_timeout` values

## Troubleshooting

### UDP Not Working

1. Verify UDP is enabled on exit agent:
   ```yaml
   udp:
     enabled: true
   ```

2. Run `muti-metroo route trace <destination-ip>` on the ingress agent and confirm a route exists, and that its exit is another agent. An agent does not relay UDP to its own exit routes.
3. Confirm the exit agent has not reached `udp.max_associations`, and keep payloads at or below `udp.max_datagram_size`.
4. Check firewall allows UDP traffic from exit

### UDP ASSOCIATE or UDP STREAM Fails with Reply Code 1

The ingress agent has no route that covers `0.0.0.0`. Run `muti-metroo route trace 0.0.0.0`. Add a default route on an exit (`muti-metroo route add 0.0.0.0/0`) or ask the mesh operator for one.

### Association Timeout

Increase `idle_timeout` for long-running UDP sessions:

```yaml
udp:
  idle_timeout: 30m
```
