# Troubleshooting

Quick solutions for frequently encountered problems.

## Agent Won't Start

### Port Already in Use

```
Error: listen tcp 0.0.0.0:4433: bind: address already in use
```

**Solution:**

```bash
# Find what's using the port
lsof -i :4433
netstat -tlnp | grep 4433

# Kill the process or use a different port
kill <PID>
```

### Configuration Error

```
Error: invalid configuration: ...
```

**Solution:**

- Check YAML syntax and indentation
- Verify quotes around values with special characters
- Check CIDR notation is valid
- Ensure required fields are present

### Certificate Not Found

```
Error: open ./certs/agent.crt: no such file or directory
```

**Solution:**

```bash
# Generate certificates
muti-metroo cert ca --cn "My CA" -o ./certs
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key \
  -o ./certs
```

### Permission Denied

```
Error: open ./data/agent_id: permission denied
```

**Solution:**

```bash
# Fix ownership
chown -R $(whoami) ./data

# Or fix permissions
chmod 700 ./data
```

## Connection Issues

### Peer Won't Connect

**Check network connectivity:**

```bash
nc -zv peer-address 4433
ping peer-address
```

**Check firewall:**

```bash
sudo iptables -L -n | grep 4433
```

**Check peer ID:**

```yaml
peers:
  - id: "correct-peer-id..." # Must match peer's agent_id
```

### TLS Handshake Failed

```
Error: tls: handshake failure
```

**Solutions:**

1. Certificate not signed by trusted CA:

   ```bash
   openssl verify -CAfile ca.crt peer.crt
   ```

2. Certificate expired:

   ```bash
   openssl x509 -enddate -noout -in agent.crt
   ```

3. Wrong hostname/IP in certificate:
   ```bash
   openssl x509 -text -noout -in agent.crt | grep -A1 "Subject Alternative Name"
   ```

## Ingress (SOCKS5) Issues

These apply to traffic from Mutiauk and from direct SOCKS clients alike, since both enter the mesh through the ingress agent's SOCKS5 listener.

### No Route to Host

```
Error: no route to 198.51.100.10
```

**Start with `route trace`** on the ingress agent. It runs the live selection logic and lists every matching CIDR along with the reason each one won or lost:

```bash
muti-metroo route trace 198.51.100.10
```

If no CIDR matches at all, the route has not reached this agent yet or has expired; work through the steps below.

**Cause:** No exit agent with matching route.

**Solutions:**

1. Check exit is running and connected (`/healthz` returns counts only;
   use `/api/dashboard` for the peer and route lists):

   ```bash
   curl http://localhost:8080/healthz       | jq '.peer_count, .route_count'
   curl http://localhost:8080/api/dashboard | jq '.peers, .routes'
   ```

2. Check exit has route configured:

   ```yaml
   exit:
     enabled: true
     routes:
       - "0.0.0.0/0"
   ```

3. Trigger route advertisement:
   ```bash
   curl -X POST http://exit-agent:8080/routes/advertise
   ```

### Connection Timeout

**Causes:**

- Slow network
- Too many hops
- Exit agent overloaded

**Solution:**

```yaml
limits:
  stream_open_timeout: 60s # Default 30s
```

### Authentication Failed

**Solutions:**

1. Verify username/password
2. Check password hash in config is correct
3. Check for special characters needing escaping

## Mutiauk Issues

Mutiauk runs on the operator workstation and connects to the ingress agent's SOCKS5 listener. Start with its own status report:

```bash
sudo mutiauk status
sudo mutiauk status --check socks5 --exit-code
```

### Cannot Reach the Ingress Agent

- Check `socks5.server` in the Mutiauk config: `host:port` for plain TCP, or a `wss://` URL for the SOCKS5 WebSocket listener.
- For `wss://`, the agent's certificate must be trusted by the workstation's system trust store. A self-signed certificate (the agent's default when `tls.cert` is not set) is rejected. Test the handshake with `curl -v https://relay.example.com:8443/socks5`.
- When the agent has `socks5.auth.enabled: true`, set `socks5.username` and `socks5.password` (or `MUTIAUK_SOCKS5_PASSWORD`) in the Mutiauk config.

### Routes or Mesh Hostnames Missing

- Confirm `autoroutes.enabled: true` in the Mutiauk config (it is off by default).
- Check what the daemon received with `sudo mutiauk route list` (routes from the agent show `SOURCE` `auto`) and `sudo mutiauk host list`.
- Mutiauk installs CIDR routes only. Destinations covered only by domain-based exit routes need a SOCKS client that sends the hostname (Chapter 9).
- Run `sudo mutiauk route trace <ip|hostname>` to see which route and mesh path a destination uses.

See Chapter 10 for TUN creation errors and DNS integration.

## Management Command Issues

### Command Rejected

```
Error: command not in whitelist
```

**Solution:**

```yaml
shell:
  whitelist:
    - bash
    - your-command
```

### Management Command Authentication Failed

Management commands use the management signing key (Ed25519), not a per-feature
password. When a request fails with `403`/`signature required` or
`invalid signature`, check the following:

1. The target agent has `management.signing_public_key` set (otherwise it
   rejects every management command request, regardless of who sent it).
2. The operator's local agent has `management.signing_private_key` set
   (otherwise the CLI cannot sign the outbound request).
3. Both keys come from the same keypair. Recover the public key from the
   private key with `muti-metroo signing-key public` and compare against the
   target's config.
4. Operator and target clocks are within ~5 minutes of each other. Every
   envelope carries a timestamp and the server enforces a window.

There is no `muti-metroo hash` workflow for management commands. See chapter 21 for the full
management key lifecycle.

## File Transfer Issues

### Path Not Allowed

```
Error: path not in allowed paths
```

**Solution:**

```yaml
file_transfer:
  allowed_paths:
    - /tmp
    - /your/path
```

### File Too Large

The default `max_file_size` is 500 MB (`524288000` bytes). Raise the limit
or set it to `0` for no cap:

```yaml
file_transfer:
  max_file_size: 1073741824 # 1 GB
  # max_file_size: 0          # Unlimited
```

## Multi-Agent Deployment

### Start Order

Start agents in the correct order:

1. **Listeners first** - Agents with `listeners:` configured
2. **Dialers second** - Agents with `peers:` configured

### Routes Not Propagating

```bash
# Trigger immediate advertisement on exit agent
curl -X POST http://exit-agent:8080/routes/advertise

# Wait and check routes on ingress
sleep 5
curl http://ingress:8080/api/dashboard | jq '.routes'   # /healthz returns counts only
```

### Reconnection After Restart

Speed up reconnection:

```yaml
connections:
  reconnect:
    initial_delay: 500ms
    max_delay: 10s
```

## Windows-Specific Issues

### Port 8080 Already in Use

```yaml
http:
  address: ":8083" # Use different port
```

### Running in Background

```powershell
Start-Process -FilePath "muti-metroo.exe" `
  -ArgumentList "run","-c","config.yaml" `
  -WindowStyle Hidden
```

### Path Issues

Use forward slashes or escaped backslashes:

```yaml
data_dir: "C:/muti-metroo/data"
# OR
data_dir: "C:\\muti-metroo\\data"
```

## Debug Mode

Enable debug logging by setting `agent.log_level` in the config:

```yaml
agent:
  log_level: "debug"
```

Then restart the agent:

```bash
muti-metroo run -c config.yaml
```

There is no `--debug` CLI flag. Log level is configuration-only so that
the deployed value survives service restarts.

## Verification Checklist

After deploying agents:

```bash
# 1. Check each agent is running
curl http://agent:8080/healthz

# 2. Check peer connections
curl -s http://agent:8080/healthz | jq '.peer_count'

# 3. Check routes exist
curl -s http://agent:8080/healthz | jq '.route_count'

# 4. Test the SOCKS5 listener directly from a host that can reach it
curl -x socks5h://127.0.0.1:1080 https://example.com

# 5. On the operator workstation, check Mutiauk
sudo mutiauk status

# 6. Check all nodes visible
curl http://agent:8080/api/nodes | jq '.nodes | length'
```
