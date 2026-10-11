---
title: muti-metroo stop
sidebar_label: stop
sidebar_position: 25
description: Ask a local or remote agent to shut down gracefully.
---

# muti-metroo stop

Ask an agent to shut down. The agent stops accepting work, gives in-flight streams up to 10 seconds to finish, closes its peer connections, and exits. The command returns as soon as the agent accepts the request; the shutdown runs after the response is sent.

The command targets the agent at `--agent` by default. Pass `-t` to stop another agent in the mesh, reached through the agent at `--agent`.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo stop [flags]
```

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-a`, `--agent` | `localhost:8080` | Agent HTTP API address used as the entry point. See [agent address](overview.md#agent-address). |
| `-t`, `--target` | none | ID of the agent to stop: a full 32-character ID or a unique prefix of one. Omit to stop the agent at `--agent`. |

## Authorization

Stop is a privileged operation. The agent at `--agent` must have `management.signing_private_key` configured, the same requirement as other runtime mutations such as `peer add` and `route add`. For a remote target, the entry agent signs the request and the target verifies it with its `management.signing_public_key` before acting. See [management configuration](../configuration/management.md).

## Confirmation prompt

Before sending the request, the command asks for confirmation:

```text
Will stop local agent gateway-a (70a9eb1d).
The process will exit; any service manager (systemd/launchd/SCM) may restart it. Continue? [y/N]:
```

For a remote target the first line names the ID you passed, for example `Will stop agent abc123de.` Answering anything other than `y` or `yes` prints `Aborted.` and exits 0. `--yes` skips the prompt. When stdin is not a terminal and `--yes` is not given, the command exits 6 without stopping anything. See [confirmation prompts](overview.md#confirmation-prompts).

## Output

```text
Shutdown triggered: agent will exit shortly
```

## Examples

Stop the local agent:

```bash
muti-metroo stop
```

Stop a remote agent by ID prefix without prompting:

```bash
muti-metroo stop -t abc123de -y
```

Stop a remote agent through an entry agent on another host:

```bash
muti-metroo stop -a 192.168.1.10:8080 -t abc123def456789012345678901234ab
```

## Service managers

A stop that completes within the 10-second drain window exits with status 0. A stop that does not finish in time is forced and exits with status 1. Whether a service manager restarts the agent depends on that exit status:

| Service backend | Clean stop (exit 0) | Forced stop (exit 1) |
| --- | --- | --- |
| Linux systemd unit (`Restart=on-failure`) | Not restarted | Restarted |
| macOS launchd (`KeepAlive` with `SuccessfulExit` false) | Not restarted | Restarted |
| Linux user service via cron `@reboot` | Started again at next boot | Started again at next boot |
| Windows service | Not restarted (the installer configures no SCM recovery actions) | Not restarted |

To remove an agent permanently, run [`muti-metroo service uninstall`](service.md) on its host.

## See Also

- [Stop API](../api/stop.md): the `POST /shutdown` endpoint behind this command
- [service](service.md): install and uninstall the agent as a system service
- [signing-key](signing-key.md): generate the signing keypair that authorizes stop
- [sleep](sleep.md): put the mesh to sleep instead of stopping agents
