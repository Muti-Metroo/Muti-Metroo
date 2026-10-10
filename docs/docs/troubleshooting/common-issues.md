---
title: Common Issues
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole fixing issues" style={{maxWidth: '180px'}} />
</div>

# Common Issues

Find your error message and fix it. Jump to the section that matches what you're seeing.

**Jump to:**
- [Agent won't start](#agent-wont-start) - port in use, config errors, certificates
- [Connection issues](#connection-issues) - peers not connecting, TLS failures
- [SOCKS5 issues](#socks5-issues) - no route, timeouts, auth failures
- [Shell/file transfer issues](#shell-issues) - command rejected, path not allowed
- [Windows-specific](#windows-specific-issues) - ports, services, paths
- [Multi-agent deployment](#multi-agent-deployment) - start order, routes not propagating

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

# Or change config
listeners:
  - address: "0.0.0.0:4434"   # Different port
```

### Configuration Error

```
Error: invalid configuration: ...
```

**Solution:**

```bash
# Validate YAML syntax
yamllint config.yaml

# Check for common issues:
# - Incorrect indentation
# - Missing quotes around values with special chars
# - Invalid CIDR notation
# - Missing required fields
```

### Certificate Not Found

```
Error: open ./certs/agent.crt: no such file or directory
```

**Solution:**

```bash
# Generate certificates
muti-metroo cert ca --cn "My CA"
muti-metroo cert agent --cn "agent-1" \
  --ca ./certs/ca.crt \
  --ca-key ./certs/ca.key

# Or fix path in config
tls:
  cert: "/absolute/path/to/agent.crt"
  key: "/absolute/path/to/agent.key"
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

# If running as service user
chown -R muti-metroo:muti-metroo /var/lib/muti-metroo
```

### Permission Denied for Agent Keys

```
Error: failed to read private key: open /opt/muti-metroo/data/agent_key: permission denied
```

This often happens when the agent was previously run as root or a different user.

**Solution:**

```bash
# Check current ownership
ls -la /opt/muti-metroo/data/

# Fix ownership (replace 'andris' with your user)
sudo chown andris:andris /opt/muti-metroo/data/agent_key
sudo chown andris:andris /opt/muti-metroo/data/agent_key.pub

# Or fix for all data files
sudo chown -R andris:andris /opt/muti-metroo/data/
```

### Stale Process from Previous Run

```
Error: bind: address already in use
```

A previous instance may still be running.

**Solution:**

```bash
# Find and kill stale processes
pgrep -la muti-metroo
pkill -f muti-metroo

# Verify ports are free
lsof -i :4433
lsof -i :1080

# On Linux, check for zombie listeners
ss -tlnp | grep muti-metroo
```

## Connection Issues

### Peer Won't Connect

**Symptoms:**
- `peers: 0` in health check
- Logs show connection attempts but no success

**Solutions:**

1. Check network connectivity:
   ```bash
   # Can you reach the peer?
   nc -zv peer-address 4433
   ping peer-address
   ```

2. Check firewall:
   ```bash
   # Is port open?
   sudo iptables -L -n | grep 4433
   ```

3. Check TLS:
   ```bash
   # Verify certificate
   openssl s_client -connect peer-address:4433
   ```

4. Check peer ID:
   ```yaml
   # Is the ID correct?
   peers:
     - id: "correct-peer-id..."  # Must match peer's agent_id
   ```

### Connection Refused

```
Error: connection refused to peer-address:4433
```

**Solutions:**

1. Peer not running or not listening
2. Wrong address/port
3. Firewall blocking connection
4. Wrong transport type

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

## SOCKS5 Issues

### No Route to Host

```
Error: no route to 1.2.3.4
```

**Cause:** No exit agent with matching route.

**Solutions:**

1. Check exit is running and connected:
   ```bash
   curl http://localhost:8080/healthz | jq '.peer_count, .route_count'
   ```

2. Check exit has route configured:
   ```yaml
   exit:
     enabled: true
     routes:
       - "0.0.0.0/0"  # Or specific CIDR
   ```

3. Trigger route advertisement:
   ```bash
   curl -X POST http://exit-agent:8080/routes/advertise
   ```

### Connection Timeout

```
Error: connection timeout
```

**Causes:**
- Slow network
- Too many hops
- Exit agent overloaded

**Solutions:**

1. Increase timeout:
   ```yaml
   limits:
     stream_open_timeout: 60s  # Default 30s
   ```

2. Reduce hop count

3. Check exit agent health

### Authentication Failed

```
Error: SOCKS5 authentication failed
```

**Solutions:**

1. Check username/password:
   ```bash
   curl -x socks5://user:password@localhost:1080 https://example.com
   ```

2. Verify password hash in config is correct

3. Check for special characters needing escaping

## Exit Issues

### DNS Resolution Failed

```
Error: DNS lookup failed for example.com
```

**Solutions:**

1. Check DNS servers are configured:
   ```yaml
   exit:
     dns:
       servers:
         - "8.8.8.8:53"
   ```

2. Check DNS servers are reachable from exit host:
   ```bash
   dig @8.8.8.8 example.com
   ```

3. Increase DNS timeout:
   ```yaml
   exit:
     dns:
       timeout: 10s
   ```

### Destination Not Allowed

```
Error: destination not in allowed routes
```

**Solution:** Add route to exit config:

```yaml
exit:
  routes:
    - "0.0.0.0/0"  # Or specific CIDR matching destination
```

## Shell Issues

### Command Rejected

```
Error: command not in whitelist
```

**Solution:** Add command to whitelist:

```yaml
shell:
  whitelist:
    - bash
    - your-command  # Add needed command
```

### Shell Authentication Failed

```
Error: auth_required
Error: invalid_credentials
```

Shell, file transfer, and dynamic config mutations are authorized by the mesh management Ed25519 signing key, not by a password.

**Solutions:**

1. Verify the local agent reached via `-a` has `management.signing_private_key` set in its config. Without it, the CLI cannot sign requests and the target rejects them as `auth_required`.

2. Verify the target agent has `management.signing_public_key` set, and that it matches the public half of the operator's signing private key. A mismatch produces `invalid_credentials`.

3. If you have lost the keypair, generate a new one with `muti-metroo signing-key generate` and redeploy the public half to every agent in the mesh.

## File Transfer Issues

### Path Not Allowed

```
Error: path not in allowed paths
```

**Solution:** Add path prefix to allowed_paths:

```yaml
file_transfer:
  allowed_paths:
    - /tmp
    - /path/to/your/files
```

### File Too Large

```
Error: file exceeds maximum size
```

**Solution:** Increase max_file_size or use 0 for unlimited:

```yaml
file_transfer:
  max_file_size: 0  # Unlimited
```

## Performance Issues

See [Performance Troubleshooting](/troubleshooting/performance) for:
- Slow connections
- High latency
- Memory usage
- CPU usage

## Windows-Specific Issues

### Port 8080 Already in Use

Windows commonly has services using port 8080. You'll see:

```
Error: listen tcp :8080: bind: Only one usage of each socket address
```

**Solution:** Use a different port:

```yaml
http:
  address: ":8083"  # Or another free port
```

Check what's using the port:

```powershell
netstat -anb | findstr ":8080"
```

### Running Agent in Background on Windows

Use the `--detach` flag to keep the agent running after you close the terminal or disconnect an SSH session:

```cmd
muti-metroo.exe run --detach -c C:\muti-metroo\config.yaml
```

This re-spawns the process outside the session's Job Object, prints the PID, and exits immediately. The agent runs until the machine reboots or you stop it manually.

```cmd
# Verify it's running
tasklist | findstr muti-metroo

# Stop the agent
taskkill /IM muti-metroo.exe /F
```

For persistent background operation that also survives reboots, install as a Windows service instead:

```cmd
muti-metroo.exe service install -c C:\muti-metroo\config.yaml
```

### Firewall Blocking Connections

Windows Firewall may block inbound or outbound connections.

```powershell
# Check if Windows Firewall is blocking
netsh advfirewall firewall show rule name=all | findstr muti-metroo

# Allow inbound on specific port
netsh advfirewall firewall add rule name="Muti Metroo" `
  dir=in action=allow protocol=tcp localport=3000

# Or allow the executable
netsh advfirewall firewall add rule name="Muti Metroo" `
  dir=in action=allow program="C:\muti-metroo\muti-metroo.exe"
```

### Path Issues with Backslashes

Windows paths use backslashes. In YAML, either escape or use forward slashes:

```yaml
# Option 1: Escaped backslashes
data_dir: "C:\\muti-metroo\\data"
tls:
  cert: "C:\\muti-metroo\\certs\\agent.crt"

# Option 2: Forward slashes (also works)
data_dir: "C:/muti-metroo/data"
```

## Multi-Agent Deployment

### Start Order Matters

When deploying multiple agents, start them in the correct order:

1. **Listeners first** - Agents that accept connections (have `listeners:` configured)
2. **Dialers second** - Agents that connect outbound (have `peers:` configured)

Example for a chain A → B → C:
1. Start Agent C (has listener, exit role)
2. Start Agent B (has listener, connects to C)
3. Start Agent A (connects to B, SOCKS5 ingress)

### Peer Not Connecting After Restart

After restarting a listener agent, dialers may take time to reconnect due to exponential backoff.

**Speed up reconnection:**

```bash
# Restart the dialer agent
pkill -f muti-metroo
./muti-metroo run -c config.yaml

# Or trigger faster reconnect by reducing max_delay
connections:
  reconnect:
    initial_delay: 500ms
    max_delay: 10s
```

### Routes Not Propagating

After starting agents, routes may take time to propagate (up to `advertise_interval`).

**Trigger immediate advertisement:**

```bash
# On exit agent
curl -X POST http://localhost:8080/routes/advertise

# Wait a few seconds for propagation
sleep 5

# Check routes on ingress agent
curl http://ingress-agent:8080/healthz | jq '.route_count'
```

### Verifying Multi-Agent Topology

Check the full topology from the dashboard API:

```bash
# List all known nodes
curl -s http://localhost:8080/api/nodes | jq '.nodes[] | {id: .short_id, name: .display_name, connected: .is_connected}'

# Check route paths
curl -s http://localhost:8080/api/dashboard | jq '.routes[] | {network, origin, hops: .hop_count, path: .path_display}'
```

### Upgrading Agents

When deploying new binary versions:

```bash
# 1. Stop the agent
pkill -f muti-metroo  # Linux/macOS
taskkill /IM muti-metroo.exe /F  # Windows

# 2. Replace binary
cp muti-metroo-linux-amd64 /opt/muti-metroo/muti-metroo
chmod +x /opt/muti-metroo/muti-metroo

# 3. Start agent
cd /opt/muti-metroo && ./muti-metroo run -c config.yaml

# 4. Verify
curl http://localhost:8080/healthz
```

For remote agents via SSH:

```bash
# Upload new binary
scp muti-metroo-linux-amd64 user@remote:/opt/muti-metroo/muti-metroo

# Stop, set permissions, restart
ssh user@remote "pkill -f muti-metroo; chmod +x /opt/muti-metroo/muti-metroo"
ssh user@remote "cd /opt/muti-metroo && nohup ./muti-metroo run -c config.yaml > agent.log 2>&1 &"
```

### Deployment Verification Checklist

After deploying agents, verify each component:

```bash
# 1. Check each agent is running
curl http://agent-a:8080/healthz  # Should show running: true

# 2. Check peer connections
curl http://agent-a:8080/healthz | jq '.peer_count'  # Should be > 0

# 3. Check routes exist
curl http://agent-a:8080/healthz | jq '.route_count'  # Should match expected routes

# 4. Test SOCKS5 proxy (if applicable)
curl --socks5 127.0.0.1:1080 https://ifconfig.me/ip

# 5. Check all nodes visible
curl http://agent-a:8080/api/nodes | jq '.nodes | length'
```

**Quick health check script:**

```bash
#!/bin/bash
for agent in "localhost:8080" "remote1:8080" "remote2:8082"; do
  echo "=== $agent ==="
  curl -s "http://$agent/healthz" | jq '{running, peers: .peer_count, routes: .route_count}'
done
```

## Debug Mode

Enable debug logging for detailed diagnostics:

```bash
muti-metroo run -c config.yaml
```

Set debug level in config:

```yaml
agent:
  log_level: "debug"
```

## Getting Help

If these solutions don't work:

1. Check logs with debug level
2. Review the specific troubleshooting guides
3. Check the protocol and limits documentation
4. Search existing issues
5. Open a new issue with:
   - Configuration (redacted)
   - Logs (debug level)
   - Steps to reproduce

## See Also

- [CLI - Status](/cli/status) - Check agent status
- [CLI - Peers](/cli/peers) - List connected peers
- [CLI - Routes](/cli/routes) - View routing table
- [CLI - Cert](/cli/cert) - Certificate management
- [API - Health](/api/health) - Health check endpoints

## Next Steps

- [Connectivity Troubleshooting](/troubleshooting/connectivity)
- [Performance Troubleshooting](/troubleshooting/performance)
- [FAQ](/troubleshooting/faq)
