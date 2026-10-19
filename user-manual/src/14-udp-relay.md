# UDP Relay

UDP relay enables SOCKS5 UDP ASSOCIATE (RFC 1928) support, allowing UDP traffic to be tunneled through the mesh network. This is primarily used for DNS and NTP traffic.

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

UDP relay uses standard SOCKS5 UDP ASSOCIATE. The client establishes a TCP connection to negotiate, then sends UDP datagrams. Mutiauk uses UDP ASSOCIATE for UDP traffic captured on its TUN interface, so UDP applications on the operator workstation need no extra setup. The examples below test UDP relay with direct SOCKS clients.

### Testing with proxychains

Configure `/etc/proxychains.conf`:

```
[ProxyList]
socks5 127.0.0.1 1080
```

DNS query through proxy:

```bash
proxychains4 dig @8.8.8.8 example.com
```

### Testing with socksify

```bash
socksify dig @8.8.8.8 example.com
```

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
- **Association lifetime**: Tied to TCP control connection

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

2. Verify exit route exists for DNS server
3. Check firewall allows UDP traffic from exit

### Association Timeout

Increase `idle_timeout` for long-running UDP sessions:

```yaml
udp:
  idle_timeout: 30m
```
