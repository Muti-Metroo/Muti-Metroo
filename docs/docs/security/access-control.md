---
title: Access Control
sidebar_position: 5
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-inspecting.png" alt="Mole controlling access" style={{maxWidth: '180px'}} />
</div>

# Access Control

Limit what authenticated users can actually do. Even with valid credentials, users can only reach destinations you allow, run commands you whitelist, and access files in directories you specify.

## Three Layers of Restriction

| Layer               | What It Controls                          | Configuration                                               |
| ------------------- | ----------------------------------------- | ----------------------------------------------------------- |
| **Exit routes**     | Which IP ranges/domains traffic can reach | [Exit Configuration](/configuration/exit)                   |
| **Shell whitelist** | Which commands can be executed            | [Shell Configuration](/configuration/shell)                 |
| **File paths**      | Which directories are accessible          | [File Transfer Configuration](/configuration/file-transfer) |

## Route-Based Access Control

Exit nodes only forward traffic to destinations that match their advertised routes. This provides implicit access control:

- Advertise `10.0.0.0/8` = only internal network accessible
- Advertise specific IPs = only those servers reachable
- Don't advertise `0.0.0.0/0` = no internet access

### How Route ACL Works

1. Client connects to destination via SOCKS5
2. Ingress agent routes to exit agent
3. Exit agent checks if destination matches advertised routes
4. **No match**: Connection rejected with "no route" error
5. **Match**: Connection established

### Route ACL Patterns

| Pattern             | Effect                                    |
| ------------------- | ----------------------------------------- |
| Internal CIDRs only | Block all internet access                 |
| Specific host IPs   | Whitelist individual servers              |
| Split exits         | Different exits for internal vs. external |
| Domain routes       | Control access by hostname                |

:::tip Configuration
See [Exit Configuration](/configuration/exit) for route setup including CIDR and domain routes.
:::

## Shell Command Whitelist

The shell whitelist determines which commands can be executed remotely:

| Whitelist Setting        | Behavior                      |
| ------------------------ | ----------------------------- |
| `[]` (empty)             | No commands allowed (default) |
| `["whoami", "hostname"]` | Only listed commands          |
| `["*"]`                  | All commands (dangerous!)     |

### Whitelist Principles

- **Command names only**: `whoami`, not `/usr/bin/whoami`
- **Arguments restricted**: Shell metacharacters (`;`, `&`, `|`, `$`, etc.) and absolute paths are blocked for safety
- **Case-sensitive**: Must match exactly

### Safe Commands for Monitoring

System info: `whoami`, `hostname`, `uptime`, `uname`
Network diagnostics: `ip`, `ping`, `traceroute`, `dig`
Resource monitoring: `df`, `du`, `ps`, `top`, `free`

### Commands to Avoid

- **System modification**: `rm`, `mv`, `cp`, `chmod`, `chown`
- **Code execution**: `sh`, `bash`, `python`, `perl`, `eval`
- **Data access**: `cat`, `less`, `grep`, `find`
- **Privilege escalation**: `sudo`, `su`

:::tip Configuration
See [Management Commands Configuration](/configuration/shell) for whitelist setup.
:::

## File Path Restrictions

File transfers are restricted to allowed directories:

| Setting             | Behavior                             |
| ------------------- | ------------------------------------ |
| `[]` (empty)        | No paths allowed (default)           |
| `["/tmp", "/data"]` | Only listed paths and subdirectories |
| `["*"]`             | All paths (dangerous!)               |

### Path Matching

Paths must start with an allowed prefix:

- `/tmp` allows `/tmp/file.txt` and `/tmp/subdir/file.txt`
- Path traversal is blocked: `/tmp/../etc/passwd` is rejected

:::tip Configuration
See [File Transfer Configuration](/configuration/file-transfer) for path restrictions and glob patterns.
:::

## Network-Level Controls

### Bind Address Restrictions

Limit which interfaces services listen on:

| Bind Address        | Access                            |
| ------------------- | --------------------------------- |
| `127.0.0.1:1080`    | Localhost only                    |
| `192.168.1.10:1080` | Specific interface only           |
| `0.0.0.0:1080`      | All interfaces (use with caution) |

### Firewall Integration

Combine with host firewall rules:

```bash
# Only allow SOCKS5 from localhost
iptables -A INPUT -p tcp --dport 1080 -s 127.0.0.1 -j ACCEPT
iptables -A INPUT -p tcp --dport 1080 -j DROP

# Only allow mesh traffic from trusted peers
iptables -A INPUT -p udp --dport 4433 -s 10.0.0.0/24 -j ACCEPT
iptables -A INPUT -p udp --dport 4433 -j DROP
```

### mTLS as Access Control

With mTLS enabled, only agents with valid certificates can connect. This provides network-level access control before any authentication occurs.

## Layered Security Example

Combine multiple controls for defense in depth:

```yaml
# Layer 1: Network binding - internal interface only
socks5:
  address: "10.0.0.5:1080"
  # Layer 2: Authentication required
  auth:
    enabled: true
    users:
      - username: "internal-user"
        password_hash: "$2a$12$..."

# Layer 3: mTLS - only valid certificates accepted
tls:
  ca: "./certs/internal-ca.crt"
  cert: "./certs/agent.crt"
  key: "./certs/agent.key"
  mtls: true

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

# Layer 4: Route restriction - internal networks only
exit:
  routes:
    - "10.0.0.0/8"

# Layer 5: Shell restriction - monitoring commands only
shell:
  enabled: true
  whitelist:
    - whoami
    - hostname
    - uptime

# Layer 6: File path restriction
file_transfer:
  enabled: true
  allowed_paths:
    - /tmp/transfers
  max_file_size: 10485760

# Layer 7: Management signing key - only operators can run shell, file transfer, or mutations
management:
  signing_public_key: "..." # Required for shell / file transfer / mutations
  # signing_private_key is held only on operator nodes
```

This configuration requires bypassing seven layers to cause damage.

## Access Control Checklist

### Minimum Security

- [ ] SOCKS5 bound to localhost or internal interface
- [ ] Management commands disabled or empty whitelist
- [ ] File transfer disabled or empty allowed_paths

### Recommended

- [ ] Authentication enabled on all services
- [ ] Exit routes limited to required networks
- [ ] Shell whitelist with only necessary commands
- [ ] File paths restricted to specific directories

### High Security

- [ ] mTLS enabled for peer connections
- [ ] Per-user SOCKS5 accounts
- [ ] Strict shell whitelist (no shells, no cat)
- [ ] Firewall rules restricting access
- [ ] Debug logging for access monitoring

## Troubleshooting

### Route Not Found

```
Error: no route to host
```

**Cause:** Destination doesn't match any advertised exit route.

**Solution:** Check exit routes with `curl http://localhost:8080/healthz | jq '.route_count'`

### Command Rejected

```
Error: command not in whitelist
```

**Cause:** Command not in shell whitelist.

**Solution:** Add to whitelist or use an allowed command.

### Path Not Allowed

```
Error: path not allowed
```

**Cause:** File path not in allowed_paths.

**Solution:** Use an allowed path or update configuration.

## Next Steps

- [Best Practices](/security/best-practices) - Production hardening
- [Authentication](/security/authentication) - Password security
- [TLS/mTLS](/security/tls-mtls) - Certificate-based access control
