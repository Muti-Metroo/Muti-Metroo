# stop

Gracefully stop a Muti Metroo agent.

```bash
muti-metroo stop [flags]
```

## Description

Asks an agent to drain in-flight streams (up to 10 seconds), close peer connections, and exit the process. The HTTP response confirms the request was accepted; the shutdown itself runs after the response is flushed.

Targets the local agent by default. Use `-t` to stop a remote agent over the mesh.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--agent` | `-a` | `localhost:8080` | Local agent API address used as the entry point |
| `--target` | `-t` | _(empty)_ | Stop a remote agent by short or full ID |
| `--yes` | `-y` | `false` | Skip the confirmation prompt |

## Examples

```bash
# Stop the local agent
muti-metroo stop

# Stop a remote agent by short ID
muti-metroo stop -t kreata

# Stop a remote agent without prompting
muti-metroo stop -t kreata -y

# Stop via a different local API
muti-metroo stop -a 192.168.1.10:8080 -t target-11
```

## Output

```
Shutdown triggered: agent will exit shortly
```

## Authorization

Stop is a privileged operation. The local agent (the entry point) must have `management.signing_private_key` configured -- the same gate used by `peer add`, `route add`, and other dynamic-config mutations. For remote targets, the local agent signs the request with `OpStop` and the target verifies using its `management.signing_public_key` before acting.

If the gate fails the agent returns HTTP 403 and does not stop.

## Service manager restart

Agents installed as a system service may be restarted automatically:

| Backend | Restart on clean exit? |
|---------|------------------------|
| Linux systemd (`Restart=on-failure`) | No -- stop is permanent |
| Linux user (cron `@reboot`) | No -- restart only on reboot |
| macOS launchd (`KeepAlive`) | **Yes** -- restarts within ~5s |
| Windows service (default) | Depends on SCM recovery policy |
| Windows Registry Run / user service | No -- restart only at next logon |

For permanent removal, run `muti-metroo service uninstall` on the host before stopping.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Stop accepted |
| 3 | Agent unreachable |
| 4 | Authentication failed (missing signing key) |
| 5 | Operation rejected |
| 6 | Confirmation required (non-TTY without `-y`) |
