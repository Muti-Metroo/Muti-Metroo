---
title: Common Issues
description: Error messages printed by Muti Metroo agents and CLI commands, what causes them, and how to fix them.
sidebar_position: 1
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-drilling.png" alt="Mole fixing issues" style={{maxWidth: '180px'}} />
</div>

# Common Issues

This page lists the errors operators see most often, grouped by where they appear. Search the page for the text of your error. For step-by-step diagnosis of peers and routes, see [Connectivity Troubleshooting](/troubleshooting/connectivity).

## Agent does not start

`muti-metroo run` prints the error and exits with a non-zero status. Under a service manager, find the same text in the service log (`journalctl -u muti-metroo` on Linux).

### Config file not found

```text
Error: failed to load config: failed to read config file: open config.yaml: no such file or directory
```

`-c` defaults to `./config.yaml` in the current directory. Pass the full path with `-c`.

### YAML syntax error

```text
Error: failed to load config: failed to parse config: yaml: line 3: mapping values are not allowed in this context
```

The file is not valid YAML, usually because of inconsistent indentation or an unquoted value containing `:`. The line number points at or just after the problem.

### Validation errors

```text
Error: failed to load config: config validation failed: validation errors:
  - listeners[0]: path is required for ws transport
```

The YAML parsed, but a value is missing or invalid. Each problem is listed on its own line with the key that caused it. Check a config without starting the agent:

```bash
muti-metroo config validate ./config.yaml
```

### Port already in use

```text
Error: failed to start agent: start HTTP server: listen tcp 127.0.0.1:8080: bind: address already in use
```

Another process, often a previous instance of the agent, holds the port. The message names the component (`HTTP server`, `listener`, or `SOCKS5`) and the address. Find the process:

```bash
lsof -nP -i :8080
```

On Windows the same condition reads:

```text
bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.
```

```powershell
netstat -ano | findstr :8080
```

Stop the other process, or change the address in the config. Port 8080 is often taken by other software on Windows.

### Certificate file not found

```text
Error: failed to start agent: start listener 0.0.0.0:4433: load TLS config: load certificate: open ./certs/agent.crt: no such file or directory
```

A `cert` or `key` path in the config does not exist. Relative paths resolve against the working directory, not the config file's directory; use absolute paths. `muti-metroo cert agent --cn <name>` writes `<name>.crt` and `<name>.key`, so check the file names as well.

If no certificate is configured at all, the agent generates a self-signed one at startup and needs no files.

### RSA certificate rejected

```text
Error: failed to start agent: start listener 0.0.0.0:4433: load TLS config: EC validation failed: certificate: RSA certificates are not supported; use EC (ECDSA) certificates
```

Listener certificates, client certificates, and CA certificates must use ECDSA keys. Certificates from `muti-metroo cert` always do. For Let's Encrypt, request an ECDSA certificate (Certbot: `--key-type ecdsa`).

### Data directory not readable or writable

```text
Error: failed to create agent: load identity: failed to read agent ID: open /var/lib/muti-metroo/agent_id: permission denied
```

```text
Error: failed to create agent: load keypair: failed to read private key: open /var/lib/muti-metroo/agent_key: permission denied
```

The data directory or the identity files belong to another user, typically because the agent once ran as root. Give the account that runs the agent ownership of the directory:

```bash
sudo chown -R "$(whoami)" /var/lib/muti-metroo
```

```text
Error: failed to create agent: load identity: failed to create data directory: mkdir /var/lib/muti-metroo: permission denied
```

The parent directory is not writable. Create the data directory in advance, or choose a writable `agent.data_dir`.

### Service fails with `status=226/NAMESPACE`

The systemd unit lists a directory in `ReadWritePaths` that does not exist. See [System Service - Troubleshooting](/deployment/system-service#troubleshooting).

## Peers do not connect

Failed connection attempts are logged at `WARN` level with the message `failed to connect to peer` and an `error` field. The agent retries with exponential backoff (1 second up to 60 seconds).

| `error` field contains | Cause | Fix |
|------------------------|-------|-----|
| `peer ID mismatch: expected <id>, got <id>` | The agent at that address has a different ID than `peers[].id`. | Correct the `id`, or use `"auto"` to accept any agent. |
| `QUIC dial failed: timeout: no recent network activity` | No QUIC response: the listener is down, the address is wrong, or UDP is blocked. | Check the address and that UDP is allowed through firewalls. Try an HTTP/2 or WebSocket listener if UDP is filtered. |
| `connect: connection refused` | Nothing listens on that TCP port (HTTP/2 or WebSocket). | Check the address, port, and transport. |
| `tls: failed to verify certificate: x509: ...` | Strict verification is on and the peer's certificate does not match the configured CA or address. | Fix the CA or certificate, or see [TLS Certificates](/configuration/tls-certificates). |
| `expected handshake response status code 101 but got 404` | WebSocket or HTTP/2 path mismatch. | Use the listener's path in the peer address, for example `wss://host:443/mesh`. |

Test a listener from the dialing host with [`muti-metroo probe`](/cli/probe). For a full checklist, see [Connectivity Troubleshooting](/troubleshooting/connectivity#peers-do-not-connect).

A related validation error:

```text
  - peers[0]: tls.ca is required when strict mode is enabled (for peer certificate verification)
```

`tls.strict: true` needs a CA to verify against. Set `tls.ca` globally or on the peer.

## SOCKS clients get errors

The SOCKS listener reports failures to the client as SOCKS5 reply codes. curl shows the code in parentheses:

```text
curl: (97) Can't complete SOCKS5 connection to 10.0.12.5. (5)
```

| Code | Meaning in Muti Metroo | What to check |
|------|------------------------|---------------|
| 1 | General failure | Agent logs on the ingress and exit. |
| 2 | Not allowed | The exit refused the destination. |
| 3 | Network unreachable | The exit host has no route to the destination network. |
| 4 | Host unreachable | DNS resolution failed, or the exit cannot reach the host. |
| 5 | Connection refused | The destination refused the TCP connection. The mesh path works. |
| 6 | Timeout | The connection attempt timed out at the exit or along the path. |

When no route matches the destination, the ingress agent does not return an error. It connects to the destination directly from its own host. If traffic leaves from the wrong place, check the routes with `muti-metroo route trace <destination>`; see [Connectivity Troubleshooting - Routing](/troubleshooting/connectivity#routing-problems).

### Authentication rejected

```text
curl: (97) User was rejected by the SOCKS5 server (1 1).
```

The username or password does not match `socks5.auth.users`. If the config uses `password_hash`, regenerate it with `muti-metroo hash`. URL-encode special characters in the password when passing it in a proxy URL.

### Host names do not resolve

With `socks5h://` or another mode that sends host names to the proxy, the ingress agent resolves the name with its own system resolver, unless the name matches a domain route, in which case the exit resolves it with `exit.dns`. A name that only internal DNS knows needs a matching `exit.domain_routes` entry on the exit. With `socks5://`, the client resolves the name itself before connecting.

## Management commands fail

`shell`, `upload`, `download`, and the dynamic configuration commands (`route add`, `peer add`, and others) are authorized with the management signing key.

### Signing key missing on the local agent

```text
Error: remote error: management signing private key not configured (set management.signing_private_key)
```

The agent that the CLI talks to (`-a`, default `localhost:8080`) has no `management.signing_private_key`, so it cannot sign the request. Set the private key on that operator agent.

### Signature rejected by the target

```text
Error: remote error: management signature rejected: management signature verification failed
```

```text
Error: upload failed: management signature verification failed
```

The target's `management.signing_public_key` is not the public half of the operator's private key. Deploy the matching public key to the target. To start over, create a new pair with `muti-metroo signing-key generate` and distribute the public key to every agent.

```text
management signing public key not configured on this agent
```

The target has no signing public key, so it rejects every privileged request. Add `management.signing_public_key` to its config.

### Command not allowed

```text
Error: remote error: failed to start session: command 'ls' is not allowed
```

The command is not in the target's `shell.whitelist`. Add it, or use `["*"]` to allow every command. See [Configuration - Shell](/configuration/shell).

### File transfer path or size rejected

```text
Error: upload failed: remote error: path not in allowed list: /tmp/report.txt
```

The path is outside the target's `file_transfer.allowed_paths`. Add the directory to the list. See [Configuration - File Transfer](/configuration/file-transfer).

```text
Error: upload failed: remote error: file too large: 104857600 bytes (max 10485760)
```

The file exceeds `file_transfer.max_file_size`. Raise the limit, or set it to `0` for no limit.

## Windows

### Paths in the config file

Backslashes are escape characters in double-quoted YAML strings. Double them, or use forward slashes:

```yaml
agent:
  data_dir: "C:/muti-metroo/data"

tls:
  cert: "C:\\muti-metroo\\certs\\agent1.crt"
```

### The agent stops when the SSH session ends

Processes started in an SSH session are terminated when the session closes. Start the agent with `run --detach`, or install it as a service. See [System Service - Windows](/deployment/system-service#windows-background-start-without-a-service).

### Windows Firewall blocks inbound peers

Allow the listener port, or the executable:

```powershell
netsh advfirewall firewall add rule name="Muti Metroo QUIC" dir=in action=allow protocol=udp localport=4433
netsh advfirewall firewall add rule name="Muti Metroo" dir=in action=allow program="C:\Program Files\muti-metroo\muti-metroo.exe"
```

## Collecting information for a bug report

Set `agent.log_level: "debug"` in the config and restart the agent to get detailed logs. Include:

- The output of `muti-metroo config show <config>` (secrets are redacted).
- The relevant log lines from each agent involved.
- The output of `muti-metroo status`, `muti-metroo peer status`, and `muti-metroo route status`.
- The version from `muti-metroo --version`.

## See Also

- [Connectivity Troubleshooting](/troubleshooting/connectivity) - Diagnosing peers and routes step by step
- [Performance Troubleshooting](/troubleshooting/performance) - Latency, throughput, and resource use
- [FAQ](/troubleshooting/faq) - Short answers to common questions
- [CLI Overview](/cli/overview) - Exit codes and global flags
- [System Service](/deployment/system-service) - Service logs and startup problems
