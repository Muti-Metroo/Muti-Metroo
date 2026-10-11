---
title: CLI overview
sidebar_label: Overview
sidebar_position: 1
description: How the muti-metroo command line is organized, plus the global flags, agent address format, output formats, confirmation prompts, and exit codes shared by every command.
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-reading.png" alt="Mole reading CLI docs" style={{maxWidth: '180px'}} />
</div>

# CLI overview

The `muti-metroo` binary is both the mesh agent and the operator command line. `muti-metroo run` starts an agent. Most other commands are clients: they connect to a running agent's HTTP API and either query it or ask it to act, locally or on a remote agent through the mesh. A few commands (`init`, `setup`, `embed-config`, `config`, `cert`, `hash`, `management-key`, `signing-key`, `probe`, `service`) work without a running agent.

Run `muti-metroo help <command>` or `muti-metroo <command> --help` for the built-in help of any command.

## Commands

Commands are grouped as they appear in `muti-metroo --help`.

### Getting started

| Command | Description |
| --- | --- |
| [`run`](run.md) | Run the mesh agent in the foreground |
| [`init`](init.md) | Create a data directory and agent ID |
| [`setup`](setup.md) | Interactive setup wizard |
| [`embed-config`](embed-config.md) | Embed a YAML config into a binary, non-interactively |

### Agent status

| Command | Description |
| --- | --- |
| [`status`](status.md) | Show agent status |
| [`probe`](probe.md) | Test connectivity to a Muti Metroo listener, or run a test listener |
| [`mesh-test`](mesh-test.md) | Test connectivity to every agent in the mesh |

### Remote operations

| Command | Description |
| --- | --- |
| [`peer`](peer.md) | Show connected peers and manage dynamic peer connections |
| [`route`](route.md) | Show the route table and manage dynamic CIDR exit routes |
| [`listener`](listener.md) | Manage dynamic transport listeners |
| [`forward`](forward.md) | Manage dynamic port-forward listeners and endpoints |
| [`host`](host.md) | Manage the mesh hostname registry |
| [`display-name`](display-name.md) | Get or set an agent's display name |
| [`ping`](ping.md) | Send ICMP echo requests through a remote agent |
| [`shell`](shell.md) | Run commands on a remote agent |
| [`upload`, `download`](file-transfer.md) | Transfer files and directories to and from a remote agent |
| [`sleep`, `wake`, `sleep-status`](sleep.md) | Put the mesh to sleep, wake it, and check sleep state |
| [`stop`](stop.md) | Gracefully stop a local or remote agent |

### Administration

| Command | Description |
| --- | --- |
| [`config`](config.md) | Validate a config file or print the effective configuration |
| [`cert`](cert.md) | Create and inspect TLS certificates |
| [`hash`](hash.md) | Generate a bcrypt hash for passwords and API tokens |
| [`management-key`](management-key.md) | Generate topology encryption keys |
| [`signing-key`](signing-key.md) | Generate Ed25519 keys that authorize privileged operations |
| [`service`](service.md) | Install, uninstall, and inspect the system service |

### Additional commands

| Command | Description |
| --- | --- |
| [`completion`](completion.md) | Generate a shell completion script |
| `help` | Show help for any command |

## Agent address

Commands that talk to an agent take `-a`, `--agent` (default `localhost:8080`), the address of the agent's HTTP API as set by `http.address` in its configuration. The flag accepts these forms:

| Form | Example | Meaning |
| --- | --- | --- |
| `host:port` | `192.168.1.10:8080` | Plain HTTP |
| `http://host[:port][/prefix]` | `http://192.168.1.10:8080` | Plain HTTP, with an optional path prefix |
| `https://host[:port][/prefix]` | `https://mesh.example.com/muti-metroo` | HTTPS, for an agent behind a TLS reverse proxy. The port defaults to 443. |

A path prefix is kept on every request, so an agent mounted under a subpath of a reverse proxy works. With `https://`, the WebSocket connections used by `shell` and `ping` switch to `wss://`. Other schemes, embedded credentials, query strings, and fragments are rejected.

```bash
muti-metroo status -a 192.168.1.10:8080
muti-metroo shell -a https://mesh.example.com/muti-metroo abc123def456789012345678901234ab whoami
```

The agent must have the HTTP API enabled (`http.enabled`). Commands that change runtime configuration (the `add`, `remove`, and `list` subcommands of `peer`, `route`, `listener`, `forward`, and `host`, plus `route trace`, `display-name`, `sleep`, `wake`, and `stop`) and every command that targets a remote agent also need `http.remote_api`, which is on by default. See [HTTP configuration](../configuration/http.md).

Remote operations are sent to the agent named by `-a` and forwarded through the mesh to the target agent. Management commands select the target with `-t`, `--target`; action commands such as `shell`, `ping`, `upload`, and `download` take the target agent ID as an argument. Without a target, management commands act on the agent named by `-a`.

## Global flags

These flags are accepted by every command.

| Flag | Default | Description |
| --- | --- | --- |
| `--token` | none | Bearer token for the agent's HTTP API, sent as `Authorization: Bearer <token>`. When the flag is not set, the `MUTI_METROO_TOKEN` environment variable is used. Required when the agent sets `http.token_hash`. |
| `-y`, `--yes` | `false` | Skip interactive confirmation prompts. See [Confirmation prompts](#confirmation-prompts). |
| `--no-color` | `false` | Disable ANSI colors in output. |
| `-h`, `--help` | | Show help for the command. |
| `-v`, `--version` | | Print the version and exit. Root command only. |

```bash
export MUTI_METROO_TOKEN='my-api-token'
muti-metroo peer list
```

## Output formats

Commands that list or report state print human-readable tables by default. Many accept `--json` for machine-readable output; the command pages list which ones. Scripts should parse the `--json` output rather than the tables.

Color is used only when stdout is a terminal. It is turned off by `--no-color` or by setting the `NO_COLOR` environment variable to any value (see [no-color.org](https://no-color.org/)).

## Confirmation prompts

Operations that are mesh-wide or hard to undo ask for confirmation before acting. The prompt is written to stderr.

| Command | Prompt |
| --- | --- |
| [`sleep`, `wake`](sleep.md) | `[y/N]` |
| [`stop`](stop.md) | `[y/N]` |
| [`route add`](route.md#route-add) with `0.0.0.0/0` or `::/0` | Requires typing `add default route`. `--allow-default-route` or `--yes` skips it. |
| [`route remove`](route.md#route-remove) | `[y/N]` |
| [`peer remove`](peer.md#peer-remove) | `[y/N]` |
| [`listener remove`](listener.md#listener-remove) | `[y/N]` |
| [`forward remove`, `forward endpoint remove`](forward.md) | `[y/N]` |
| [`host remove`](host.md#host-remove) | `[y/N]` |
| [`host suffix remove`](host.md#host-suffix-remove) | `[y/N]`, only when removing the last remaining suffix |
| [`display-name set ""`](display-name.md#display-name-set) | `[y/N]` before reverting to the configured name |

Answering anything other than `y` or `yes` prints `Aborted.` and exits with status 0 without making a change.

`-y`, `--yes` answers yes to every prompt. When stdin is not a terminal (cron, CI, a pipe) and `--yes` is not given, the command does not prompt; it fails with exit code 6.

```bash
muti-metroo route remove 10.0.0.0/8 --yes
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success, or a confirmation prompt was declined |
| 1 | Generic error |
| 2 | Usage error: unknown command or flag, wrong number of arguments, invalid flag value |
| 3 | Agent unreachable: the HTTP API could not be contacted |
| 4 | Authentication failed: bearer token missing or rejected |
| 5 | Operation rejected by the agent, or a config file failed validation |
| 6 | Confirmation required: stdin is not a terminal and `--yes` was not given |

Some commands use additional exit codes; their pages say so. For example, `shell` exits with the remote command's exit code.

In the current release, code 4 is rarely returned: when the agent rejects the token, most commands report `unexpected status: 401` or a response decoding error and exit with code 1. Scripts that need to detect a bad token should check for `401` in the error output.

## Deprecated commands and flags

These forms still work but print a warning to stderr and will be removed in a future release.

| Deprecated | Replacement | Notes |
| --- | --- | --- |
| `muti-metroo peers` | [`muti-metroo peer status`](peer.md#peer-status) | Same output and flags. Hidden from `--help`. |
| `muti-metroo routes` | [`muti-metroo route status`](route.md#route-status) | Same output and flags. Hidden from `--help`. |
| `-t` meaning `--timeout` on `mesh-test`, `probe`, `shell`, `upload`, `download`, `ping` | `-w`, `--timeout` | On every other command `-t` means `--target`. A later major release will make `-t` mean `--target` on these commands too. |

```bash
# Before
muti-metroo upload abc123def456789012345678901234ab ./file.txt /tmp/file.txt -t 5m
# After
muti-metroo upload abc123def456789012345678901234ab ./file.txt /tmp/file.txt -w 5m
```

## See Also

- [run](run.md): start an agent from a config file
- [setup](setup.md): build a configuration interactively
- [HTTP API overview](../api/overview.md): the endpoints the CLI calls
- [HTTP configuration](../configuration/http.md): set the API address and `token_hash`
- [Signing key](signing-key.md): authorize the privileged operations that remote commands perform
