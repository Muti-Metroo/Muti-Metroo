---
title: Interactive Setup
description: Walk through the muti-metroo setup wizard step by step, from roles and transports to keys, config delivery, and service installation.
sidebar_position: 2
---

<div style={{textAlign: 'center', marginBottom: '2rem'}}>
  <img src="/img/mole-idea.png" alt="Mole with bright idea" style={{maxWidth: '180px'}} />
</div>

# Interactive Setup

`muti-metroo setup` asks a series of questions and produces a ready-to-run agent: a configuration file (or a binary with the configuration embedded), the agent identity in the data directory, and optionally a system or user service. This page walks through every prompt. For the command synopsis and flags, see [`setup`](/cli/setup).

## Running the wizard

```bash
muti-metroo setup
```

Every prompt shows its default in square brackets. Press Enter to accept it. Menus take the number of an option, and the role menu takes a comma-separated list such as `1,3`.

To edit the configuration embedded in an existing binary, pass the binary with `-c`. See [Editing embedded configuration](#editing-embedded-configuration).

:::tip[Minimal path to a standalone proxy]
For a single agent that is both SOCKS5 entry point and exit, select roles `1,3` (Ingress and Exit), and accept the defaults everywhere else. At the HTTP API authentication prompt, either set a token or confirm that you want to continue without one.
:::

## Wizard steps

The wizard runs the steps below in order. Steps that do not apply to the selected roles are skipped.

### Step 1: Basic setup

| Prompt             | Default         | Notes                                                                                         |
| ------------------ | --------------- | --------------------------------------------------------------------------------------------- |
| `Config File Path` | `./config.yaml` | Must end in `.yaml` or `.yml`, and its parent directory must exist.                           |
| `Data Directory`   | `./data`        | Holds the agent identity and E2E encryption keys.                                             |
| `Display Name`     | empty           | Human-readable name shown in the dashboard and CLI. Press Enter to show the Agent ID instead. |

If a valid Muti Metroo configuration already exists at the config path, the wizard prints `[INFO] Found existing configuration, using values as defaults.` and every later prompt starts from the existing values. See [Re-running the wizard](#re-running-the-wizard). If a file exists there but does not parse as a configuration, the wizard asks `Overwrite it?` (default No) and cancels if you decline.

### Step 2: Agent role

Select one or more roles:

| Option      | Effect on the generated config                                              |
| ----------- | --------------------------------------------------------------------------- |
| **Ingress** | Enables the SOCKS5 proxy (Step 6).                                          |
| **Transit** | No extra settings. Every agent relays traffic for its peers.                |
| **Exit**    | Enables exit routing (Step 7).                                              |

The default is Transit. An agent with only the Transit role skips Steps 6, 7, 9, and 10. See [Agent Roles](/concepts/agent-roles).

### Step 3: Inbound listener (optional)

`Accept inbound peer connections?` defaults to No. Answer No for SOCKS5-only clients, laptops behind NAT, or any agent that only dials out. The generated config then has no `listeners` section, and outbound peers are still configured in Step 5.

When you answer Yes:

| Prompt                                       | Default        | Notes                                                                                   |
| -------------------------------------------- | -------------- | --------------------------------------------------------------------------------------- |
| `Transport Protocol`                         | QUIC           | QUIC, HTTP/2, or WebSocket. See [Transports](/concepts/transports).                     |
| `Listen Address`                             | `0.0.0.0:4433` | `host:port` to bind.                                                                    |
| `HTTP Path`                                  | `/mesh`        | HTTP/2 and WebSocket only. Must start with `/`.                                         |
| `Is TLS terminated by a reverse proxy?`      | No             | HTTP/2 and WebSocket only. Yes sets `plaintext: true` on the listener.                  |
| `Advertise this listener so other agents...` | No             | Yes asks for a public `host:port` and adds an `advertise` block to the listener.        |

The advertised address must be reachable by other agents: loopback and unspecified addresses (`127.0.0.1`, `0.0.0.0`, `::`) are rejected. See [Listeners](/configuration/listeners).

### Step 4: TLS configuration

All peer traffic is end-to-end encrypted regardless of this choice. Transport TLS adds certificate-based peer verification on top.

| Option                                        | Effect                                                                                       |
| --------------------------------------------- | -------------------------------------------------------------------------------------------- |
| **Self-signed certificates** (default)        | No `tls` section is written. The agent generates a self-signed certificate at startup.       |
| **Strict TLS with CA verification**           | Writes CA, certificate, and key PEM inline under `tls` and sets `strict: true`.              |

Strict TLS offers two sub-options:

- **Paste CA certificate, agent certificate, and key**: paste each PEM block, ending each with an empty line. The private key must be an EC key.
- **Generate from CA private key**: paste an ECDSA P-256 CA private key. The wizard asks for `Validity period (days)` (default `365`) and `Agent Common Name` (default `muti-metroo`), derives the CA certificate, and issues an agent certificate valid for the common name, `localhost`, `127.0.0.1`, and `::1`.

When the existing config already carries TLS material, a third option, `Keep existing TLS configuration`, is offered and selected by default. See [TLS Certificates](/configuration/tls-certificates).

### Step 5: Peer connections

`Add peer connections?` defaults to No. For each peer:

| Prompt                                  | Default                        | Notes                                                                                     |
| --------------------------------------- | ------------------------------ | ----------------------------------------------------------------------------------------- |
| `Peer Address (host:port)`              | none                           | Required.                                                                                 |
| `Pin to specific agent ID?`             | No                             | Yes asks for the peer's Agent ID. No writes `id: auto`, which accepts any agent ID.       |
| `Use same transport as listener (...)?` | Yes                            | Shown when a listener was configured. Without a listener, a transport menu is shown (default QUIC). |
| `Enable strict TLS verification?`       | No                             | Yes sets `tls.strict: true` for this peer. Requires a CA.                                 |
| `HTTP Path`                             | `/mesh`                        | HTTP/2 and WebSocket peers only.                                                          |

The wizard then probes the peer for up to 10 seconds. On success it prints the remote agent's name and ID and the round-trip time, and adds the peer. On failure it offers:

1. `Continue anyway (I'll set up the listener later)`: keep the peer (default).
2. `Retry the connection test`
3. `Re-enter peer configuration`
4. `Skip this peer`

See [Peers](/configuration/peers).

### Step 6: SOCKS5 proxy

Shown when the Ingress role is selected.

| Prompt                   | Default          | Notes                                                                                                   |
| ------------------------ | ---------------- | ------------------------------------------------------------------------------------------------------- |
| `Listen Address`         | `127.0.0.1:1080` | `host:port` for the SOCKS listener.                                                                     |
| `Enable authentication?` | No               | Yes asks for one username and a password (at least 8 characters). The wizard writes the password in plaintext to the deprecated `password` key; replace it with a bcrypt `password_hash` from [`muti-metroo hash`](/cli/hash) to keep it out of the file. |

See [SOCKS5](/configuration/socks5).

### Step 7: Exit node configuration

Shown when the Exit role is selected.

- **Routes**: enter one CIDR per line and an empty line to finish. If you enter none, the wizard writes `0.0.0.0/0` and `::/0`.
- **Domain routes**: enter one domain pattern per line (for example `api.internal.example.com` or `*.example.com`), and an empty line to finish. Optional.

The generated `exit` section also sets `dns.servers` to `8.8.8.8:53` and `1.1.1.1:53` with a `5s` timeout. Edit or remove `dns.servers` afterwards to use the system resolver instead. See [Exit](/configuration/exit).

### Step 8: Monitoring and logging

| Prompt                              | Default | Notes                                                                                  |
| ----------------------------------- | ------- | -------------------------------------------------------------------------------------- |
| `Log Level`                         | Info    | Debug, Info, Warning, or Error.                                                        |
| `Enable HTTP management API?`       | Yes     | Writes an `http` section listening on `:8080`.                                         |
| `Enable HTTP API authentication?`   | No      | Yes asks for a bearer token (at least 8 characters) and stores its bcrypt hash in `http.token_hash`. |

If you decline authentication, the wizard warns that the dashboard and topology endpoints on `:8080` are readable by anyone who can reach the host, and asks `Continue without HTTP API authentication?` (default No). Answering No stops the wizard without writing anything, so answer Yes here if you plan to protect the API another way. See [HTTP](/configuration/http).

### Step 9: Management commands

Skipped for transit-only agents. `Enable management commands?` defaults to No. When enabled, choose a whitelist mode:

- `Allow all commands`: writes `whitelist: ["*"]`.
- `Custom whitelist`: enter command names one per line. At least one is required.
- `No commands (lockdown - configure later via config file)`: writes an empty whitelist.

Remote command execution is authorized by the signing key from Step 11, not by a per-feature password. See [Shell](/configuration/shell).

### Step 10: File transfer

Skipped for transit-only agents. `Enable file transfer?` defaults to No. When enabled:

- `Max File Size (MB, 0 = unlimited)`: default `500`.
- `Path Access`: `Allow all paths (no restrictions)` writes `allowed_paths: ["*"]`; `Restrict to specific directories` asks for one path per line.

File transfer is authorized by the signing key from Step 11. See [File Transfer](/configuration/file-transfer).

### Step 11: Management and signing keys

This step configures two independent keypairs:

- **Management key (X25519)**: encrypts topology data so only operators can view the mesh structure.
- **Signing key (Ed25519)**: authorizes shell, file transfer, sleep/wake, and dynamic configuration changes. An agent without the signing public key rejects these operations.

| Option                              | Behavior                                                                                                                                                                                                                                  |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Generate new keypairs`             | Generates both keypairs and asks `Is this an operator/management node?` (default No). An operator node keeps all four keys in its config; a field agent keeps only the two public keys. The public keys are printed so you can copy them into other agents. |
| `Enter existing keys` (default)     | Asks `Which keys do you have?`: both private keys (operator node, public keys are derived), both public keys (field agent), or `I don't have keys yet (generate new)`.                                                                      |
| `Skip`                              | Writes no `management` section.                                                                                                                                                                                                           |

Private keys belong only in operator configs. When the existing config already has keys, the wizard first asks `Keep existing keys?` (default Yes). See [Management](/configuration/management).

### Configuration delivery

| Option                                       | Result                                                                                                                       |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `Save to config file (traditional)` (default) | Writes the config file from Step 1 with mode `0600`.                                                                        |
| `Embed in binary (single-file deployment)`   | Asks for `Output binary path` (default: the system temp directory plus the service name) and writes a copy of the running binary with the config embedded. |

Both options then ask for `Service name` (default `muti-metroo`; letters, digits, hyphens, and underscores only). The name is used for service installation and, when embedding, for the default output file name.

When embedding, the agent ID and private key are placed in the embedded config and `data_dir` is removed, so the binary needs no data directory. A copy of the config is also saved to the config file path. See [Embedded Configuration](/deployment/embedded-config).

### Service installation

After the summary, the wizard offers to install the agent as a service. If a service with the chosen name is already installed, it offers `Update binary and restart service?` instead.

| Platform and privileges    | What the wizard offers                                                                                                                                     |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Linux as root              | systemd service, with the binary copied to `/usr/local/bin/<service-name>`.                                                                                |
| Linux as a regular user    | User service with automatic backend detection, `systemd --user`, or cron `@reboot` with nohup; or the command for a system-level install.                  |
| macOS as root              | launchd service, with the binary copied to `/usr/local/bin/<service-name>`.                                                                                |
| macOS as a regular user    | Prints the `sudo muti-metroo service install` command to run later.                                                                                        |
| Windows as Administrator   | Windows Service, with the binary copied to `%ProgramFiles%\<service-name>\<service-name>.exe`.                                                             |
| Windows as a regular user  | User service through a Registry Run key that starts `muti-metroo.dll` at logon, or the command for a Windows Service install.                              |

The service reads the config file at the path chosen in Step 1, so pick a permanent location such as `/etc/muti-metroo/config.yaml` when you intend to install a service. For a Windows user service set up later from the command line:

```powershell
muti-metroo service install --user --dll C:\Users\operator\muti-metroo\muti-metroo.dll -c C:\Users\operator\muti-metroo\config.yaml
```

See [System Service](/deployment/system-service) and [DLL Mode](/deployment/dll-mode).

## Example session

The session below configures an Ingress and Exit agent with a QUIC listener, one outbound peer that is not reachable yet, and two exit routes. Typed answers follow the prompts; empty answers accept the default.

```text
$ muti-metroo setup

================================================================================
                            Muti Metroo Setup Wizard
================================================================================
                        Userspace Mesh Networking Agent

This wizard will help you configure Muti Metroo.
Press Enter to accept default values shown in [brackets].
Tip: point it at an existing config to edit it; current values become the defaults.


--------------------------------------------------------------------------------
Step 1/11: Basic Setup
--------------------------------------------------------------------------------
Configure the essential paths for your agent.

Config File Path [./config.yaml]:
Data Directory [./data]:
  Data directory stores agent identity and encryption keys.
Display Name (press Enter to use Agent ID): gateway-1

--------------------------------------------------------------------------------
Step 2/11: Agent Role
--------------------------------------------------------------------------------
Select the roles this agent will perform.
You can select multiple roles.


  1. [ ] Ingress (SOCKS5 proxy entry point)
  2. [x] Transit (relay traffic between peers)
  3. [ ] Exit (connect to external networks)

Select Roles (comma-separated) [2]: 1,3

--------------------------------------------------------------------------------
Step 3/11: Inbound Listener (optional)
--------------------------------------------------------------------------------
Does this agent accept inbound peer connections?
Answer No for SOCKS5-only clients, laptops behind NAT, or any agent
that only dials out to other peers. Outbound peer dials are configured
in the next step regardless.

Accept inbound peer connections? [y/N]: y

> 1. QUIC - Best performance, requires UDP port access
  2. HTTP/2 - Works through firewalls that only allow TCP/443
  3. WebSocket - Works through HTTP proxies and CDNs

Transport Protocol [1]:
Listen Address [0.0.0.0:4433]:
Advertise this listener so other agents can discover and dial it? [y/N]:

--------------------------------------------------------------------------------
Step 4/11: TLS Configuration
--------------------------------------------------------------------------------
All traffic is end-to-end encrypted (X25519 + ChaCha20-Poly1305).
Transport TLS adds certificate-based peer verification on top of E2E encryption.


> 1. Self-signed certificates (Recommended)
  2. Strict TLS with CA verification (Advanced)

Certificate Setup [1]:
[OK] TLS certificates will be auto-generated at startup.
    E2E encryption provides security - TLS verification is optional.

--------------------------------------------------------------------------------
Step 5/11: Peer Connections
--------------------------------------------------------------------------------
Configure connections to other mesh agents.

Add peer connections? [y/N]: y

--------------------------------------------------------------------------------
Peer #1
--------------------------------------------------------------------------------
Peer Address (host:port): 192.0.2.10:4433
Pin to specific agent ID? [y/N]: n
Use same transport as listener (QUIC)? [Y/n]:
Enable strict TLS verification? (requires CA-signed certs) [y/N]:

[INFO] Testing connectivity to peer...
[WARNING] Could not connect: Connection timed out - firewall may be blocking
[INFO] The listener may not be running yet, or a firewall may be blocking.

> 1. Continue anyway (I'll set up the listener later)
  2. Retry the connection test
  3. Re-enter peer configuration
  4. Skip this peer

What would you like to do? [1]:
Add another peer? [y/N]:

--------------------------------------------------------------------------------
Step 6/11: SOCKS5 Proxy
--------------------------------------------------------------------------------
Configure the SOCKS5 ingress proxy.

Listen Address [127.0.0.1:1080]:
Enable authentication? [y/N]:

--------------------------------------------------------------------------------
Step 7/11: Exit Node Configuration
--------------------------------------------------------------------------------
Configure this agent as an exit node.
It will allow traffic to specified networks.

Allowed Routes (CIDR):
  Default: 0.0.0.0/0 and ::/0 (enter empty line to keep defaults)

Route (or empty to finish): 10.0.0.0/8
Route (or empty to finish): 192.168.0.0/16
Route (or empty to finish):

Route traffic by domain name. Examples: api.internal.corp, *.example.com
Use *.domain.tld for single-level wildcards.

Domain (or empty to finish):

--------------------------------------------------------------------------------
Step 8/11: Monitoring & Logging
--------------------------------------------------------------------------------
Configure monitoring, logging, and the HTTP management API.


  1. Debug (verbose)
> 2. Info (recommended)
  3. Warning
  4. Error (quiet)

Log Level [2]:
Enable HTTP management API? (health checks, dashboard, CLI remote commands) [Y/n]:
Enable HTTP API authentication? [y/N]:

  WARNING: HTTP API will bind on :8080 without a bearer token.
  Topology and dashboard endpoints will be readable by anyone who can
  reach this host on that address. Mutation endpoints remain protected by
  the management signing key.
Continue without HTTP API authentication? [y/N]: y

--------------------------------------------------------------------------------
Step 9/11: Management Commands
--------------------------------------------------------------------------------
Management commands allow executing commands remotely on this agent.

Enable management commands? [y/N]:

--------------------------------------------------------------------------------
Step 10/11: File Transfer
--------------------------------------------------------------------------------
File transfer allows uploading and downloading files to/from this agent.
Files are streamed directly through the mesh network.

Enable file transfer? [y/N]:

--------------------------------------------------------------------------------
Step 11/11: Management & Signing Keys
--------------------------------------------------------------------------------
Two independent keypairs control mesh management.
  - Management key (X25519): encrypts topology so only operators can view it.
  - Signing key (Ed25519): authorizes shell, file transfer, sleep/wake, and
    dynamic config changes. Agents without the signing public key cannot
    accept these privileged operations.
Recommended for any non-trivial deployment.


  1. Generate new keypairs
> 2. Enter existing keys
  3. Skip

Key Setup [2]: 3

--------------------------------------------------------------------------------
Configuration Delivery
--------------------------------------------------------------------------------
Choose how to deploy the configuration.


> 1. Save to config file (traditional)
  2. Embed in binary (single-file deployment)

Delivery method [1]:

Service name [muti-metroo]:

[OK] Generated new E2E encryption keypair

--------------------------------------------------------------------------------
[OK] Setup Complete!
--------------------------------------------------------------------------------

  Display Name:    gateway-1
  Agent ID:        fe981f063416aeb04e0156857e9a5f6f
  E2E Public Key:  8b7e7240b01e001808659c9be485ae07a44addbc81cccb7f76c1cc573df16517
  Config file:     ./config.yaml
  Data dir:        ./data

  Listener:        quic://0.0.0.0:4433
  SOCKS5:          127.0.0.1:1080
  Exit routes:     [10.0.0.0/8 192.168.0.0/16]
  HTTP API:        http://:8080

  To start the agent:
    muti-metroo run -c ./config.yaml
```

The service installation prompts for the current platform follow the summary.

The session writes this `config.yaml`:

```yaml
# Muti Metroo Configuration
# Generated by setup wizard

agent:
    display_name: gateway-1
    data_dir: ./data
listeners:
    - transport: quic
      address: 0.0.0.0:4433
peers:
    - id: auto
      transport: quic
      address: 192.0.2.10:4433
socks5:
    enabled: true
    address: 127.0.0.1:1080
    max_connections: 1000
exit:
    enabled: true
    routes:
        - 10.0.0.0/8
        - 192.168.0.0/16
    dns:
        servers:
            - 8.8.8.8:53
            - 1.1.1.1:53
        timeout: 5s
http:
    enabled: true
    address: :8080
```

## After the wizard

With file delivery, the wizard has written:

- The config file, readable only by its owner (mode `0600`).
- `agent_id`, `agent_key`, and `agent_key.pub` in the data directory. Running [`init`](/cli/init) separately is not needed.

No certificate files are written. With self-signed TLS the agent generates its certificate at startup; with strict TLS the PEM blocks are inline in the config.

Start the agent, unless the wizard installed and started a service:

```bash
muti-metroo run -c ./config.yaml
```

Check that it is up and that the SOCKS proxy reaches a host inside one of the exit routes:

```bash
curl http://localhost:8080/health
muti-metroo status
curl -x socks5h://localhost:1080 http://192.168.1.10/
```

`/health` returns `OK` once the agent is running.

## Re-running the wizard

Run `muti-metroo setup` again and enter the path of the existing config at the first prompt. The wizard loads it and uses its values as defaults. In addition:

- Existing peers are listed, and `Keep these existing peer connections?` defaults to Yes.
- Existing TLS material, SOCKS5 credentials, the HTTP API token hash, and management keys each get a "keep existing" prompt that defaults to Yes.
- Exit routes and domain routes are kept when you enter none.
- Sections the wizard does not ask about, such as `forward`, `routing`, `limits`, `udp`, `icmp`, `sleep`, mesh hosts, and any listeners after the first, are carried over unchanged.

The file is then rewritten in the wizard's format, so comments in a hand-edited config are not preserved.

## Editing embedded configuration

To change the configuration embedded in a deployed binary, pass the binary to `-c`:

```bash
muti-metroo setup -c /usr/local/bin/my-agent
```

In this mode the wizard:

1. Loads the embedded configuration as defaults.
2. Asks only for the display name in Step 1, since there is no config file or data directory.
3. Keeps the agent identity from the embedded config, or generates a new one in memory if none is present.
4. Writes the new configuration back into the same binary and skips the delivery step. The service name is taken from the binary's file name.
5. Offers to restart the service if one with that name is installed.

To update an installed service manually:

```bash
sudo systemctl stop my-agent
sudo muti-metroo setup -c /usr/local/bin/my-agent
sudo systemctl start my-agent
```

Windows DLL files cannot be passed to `setup -c`. Use a separate config file with the DLL instead.

## See Also

- [`setup`](/cli/setup): command synopsis and flags.
- [Quick Start](/getting-started/quick-start): write a configuration by hand instead of using the wizard.
- [Your First Mesh](/getting-started/first-mesh): connect a second agent to the one you configured.
- [Configuration Overview](/configuration/overview): every section the wizard writes, with all keys and defaults.
- [Embedded Configuration](/deployment/embedded-config): single-file deployments built from the delivery step.
- [System Service](/deployment/system-service): manage the service the wizard installs.
