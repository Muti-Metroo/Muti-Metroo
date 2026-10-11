# Setting Up the Shared Host

The shared host is a single Linux box reachable by every operator. This chapter walks through standing it up from a clean install.

The goal at the end of this chapter:

- `muti-metroo` is running as a systemd service. Its HTTP API is protected by a bearer token. Its config holds the management signing private key, so it can issue privileged requests (route changes, management commands, file transfer, sleep/wake) on behalf of operators. SOCKS5 is exposed locally and over WebSocket if operators are behind picky firewalls.
- `muti-metroo-manager` is running, fronted by your TLS reverse proxy, and shows a working **Downloads** tab.
- You have the bearer token in your password manager. That is the only thing operators need.

## Step 1: Install Muti Metroo

Place the `muti-metroo-linux-amd64` binary on the shared host -- this is the bootstrap, so it has to be brought in out-of-band (via your release-artifacts source). Then on the host:

```bash
chmod +x muti-metroo-linux-amd64
sudo mv muti-metroo-linux-amd64 /usr/local/bin/muti-metroo
muti-metroo --version
```

## Step 2: Generate Keys and a Token

Three secrets live on the shared host. None of them ever has to leave it (apart from the public halves, which you copy to every other agent in the mesh, and the bearer token, which operators paste into Muti Metroo Manager).

| Secret                               | Purpose                                                                                                                                                                                                     | Generate with                   |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------- |
| HTTP bearer token                    | Operators paste this into Muti Metroo Manager. Mutiauk stores it in its config.                                                                                                                                   | `muti-metroo hash`                    |
| Management signing keypair (Ed25519) | Authorises management commands, file transfer, route/peer/forward mutations, sleep/wake. The shared host signs with the **private** half; every agent in the mesh has the **public** half so it can verify. | `muti-metroo signing-key generate`    |
| Management keypair (X25519)          | Encrypts topology and mesh-host data so transit nodes cannot read it.                                                                                                                                       | `muti-metroo management-key generate` |

```bash
# Bearer token. `muti-metroo hash` only prints the bcrypt hash (the cleartext
# you typed is never echoed back). Pick a strong cleartext password
# yourself first -- e.g. `openssl rand -hex 32` -- type it at the prompt,
# and save BOTH values: the cleartext is what operators paste into
# Muti Metroo Manager, the printed hash goes into the agent config as
# `http.token_hash`.
muti-metroo hash                              # default cost 12 (OWASP 2024)

# Ed25519 signing keypair: both halves go into the shared host's config.
# Only the public half goes into every other agent in the mesh.
muti-metroo signing-key generate --out ./secrets/

# X25519 management keypair: same split.
muti-metroo management-key generate --out ./secrets/
```

`--out <dir>` writes the keys to disk with safe permissions (0600 for
private, 0644 for public) instead of echoing them to the terminal --
preferred for production rollouts. The cleartext bearer token is what
operators paste into Muti Metroo Manager; the bcrypt hash is what goes in the agent
config under `http.token_hash`.

## Step 3: Run the Setup Wizard

The fastest way to get a working config is `muti-metroo setup`. It generates certificates, asks you which roles the agent plays, and writes the YAML.

```bash
sudo mkdir -p /etc/muti-metroo /var/lib/muti-metroo
sudo muti-metroo setup -c /etc/muti-metroo/config.yaml
```

When the wizard asks about agent role, it offers a multi-select of **Ingress**, **Transit**, and **Exit**. For the shared host pick all three (or at minimum **Ingress** + **Transit**) so the agent can serve operator SOCKS5 traffic, forward to peer relays, and optionally act as an exit.

After the wizard you should have something close to the following at `/etc/muti-metroo/config.yaml`. Fill in the secrets generated in Step 2:

```yaml
agent:
  id: "auto"
  display_name: "shared-host"
  data_dir: "/var/lib/muti-metroo"
  log_level: "info"

listeners:
  - transport: quic
    address: "0.0.0.0:4433"

peers: [] # Add upstream relays / exit nodes here

socks5:
  enabled: true
  address: "0.0.0.0:1080"
  auth:
    enabled: true
    users:
      - username: "ops"
        password_hash: "$2a$12$..." # muti-metroo hash (default cost 12)
  websocket:
    enabled: true
    address: "0.0.0.0:8443"
    path: "/socks5"
    plaintext: true # behind a reverse proxy that terminates TLS

http:
  enabled: true
  address: ":8080"
  token_hash:
    "$2a$12$..." # muti-metroo hash; this is the bearer token
    # everyone shares.

management:
  signing_public_key: "..." # from muti-metroo signing-key generate
  signing_private_key:
    "..." # the matching private half. Only set on
    # the shared host (the agent that signs
    # operator requests). Other mesh agents
    # leave this empty -- they only verify.
  public_key: "..." # from muti-metroo management-key generate
  private_key: "..." # ditto, only on the shared host.
```

The wizard creates the data directory and self-signed certs at `/var/lib/muti-metroo/certs/`.

## Step 4: Install as a systemd Service

```bash
sudo muti-metroo service install -c /etc/muti-metroo/config.yaml
sudo systemctl status muti-metroo
```

The service starts immediately and is enabled on boot. Logs go to `journald`:

```bash
sudo journalctl -u muti-metroo -f
```

If you cannot get root on this host, `muti-metroo service install --user -c ~/muti-metroo/config.yaml` registers a user-level service instead. It auto-picks `systemd --user` when available (logs via `journalctl --user -u muti-metroo`) and falls back to a `@reboot` cron entry on hosts without a user systemd manager. Force a specific backend with `--user-method=systemd|cron`. To keep a `systemd --user` unit running after logout, the host admin needs to run `sudo loginctl enable-linger <user>` once.

Quick sanity check:

```bash
curl -s http://localhost:8080/health
# OK
```

## Step 5: Install Muti Metroo Manager

Place the `muti-metroo-manager-linux-amd64` binary on the shared host (same out-of-band route as Step 1), then:

```bash
chmod +x muti-metroo-manager-linux-amd64
sudo mv muti-metroo-manager-linux-amd64 /usr/local/bin/muti-metroo-manager
```

Run it as a non-root user. A minimal systemd unit (`/etc/systemd/system/muti-metroo-manager.service`):

```ini
[Unit]
Description=Muti Metroo Manager Dashboard
After=network.target muti-metroo.service

[Service]
Type=simple
User=muti-metroo-manager
WorkingDirectory=/var/lib/muti-metroo-manager
Environment=METROO_MANAGER_AGENT_TOKEN_FILE=/etc/muti-metroo-manager/token
ExecStartPre=/usr/local/bin/muti-metroo-manager check -addr 127.0.0.1:3000 -agent http://127.0.0.1:8080 -agent-token-file /etc/muti-metroo-manager/token
ExecStart=/usr/local/bin/muti-metroo-manager -addr 127.0.0.1:3000 -agent http://127.0.0.1:8080 -agent-token-file /etc/muti-metroo-manager/token -log-format json
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`ExecStartPre=muti-metroo-manager check` validates config + agent reachability without binding the listener; if the agent is misconfigured or unreachable the unit fails fast instead of looping in `Restart=on-failure`. `-log-format json` produces one JSON object per line for journald / SIEM ingestion. If the agent has no `http.token_hash`, drop both `-agent-token-file` lines.

```bash
sudo useradd -r -s /usr/sbin/nologin -d /var/lib/muti-metroo-manager muti-metroo-manager
sudo mkdir -p /var/lib/muti-metroo-manager && sudo chown muti-metroo-manager: /var/lib/muti-metroo-manager
sudo systemctl daemon-reload
sudo systemctl enable --now muti-metroo-manager
```

Reverse-proxy `https://muti-metroo.example.com -> 127.0.0.1:3000` with whatever TLS terminator you already use (Caddy, nginx, Traefik). Operators visit `https://muti-metroo.example.com` in their browser, paste the bearer token when prompted, and the dashboard comes alive.

Muti Metroo Manager does not need a token of its own at startup -- the operator paste at the login dialog is enough. If you would prefer the dashboard to authenticate as the agent without a per-session paste (for example to support unattended dashboards), point `-agent-token-file` at a 0600-mode file, or set `METROO_MANAGER_AGENT_TOKEN` in the systemd unit. Most teams skip that.

## Step 6: Drop In the Downloads Bundle

The **Downloads** tab in Muti Metroo Manager only appears if a downloads bundle is sitting next to the binary.

The bundle (`muti-metroo-downloads-bundle.zip`) is built by the Muti Metroo Manager release process and contains every Muti Metroo and Mutiauk binary for every supported platform, plus the user and operator manuals. Bring it in alongside the muti-metroo-manager binary from your release-artifacts source.

```bash
sudo install -m 0644 muti-metroo-downloads-bundle.zip /usr/local/bin/muti-metroo-downloads-bundle.zip
sudo systemctl restart muti-metroo-manager
```

Refresh the dashboard. A **Downloads** tab now appears in the top navigation. From here onwards, telling a new operator "go to the dashboard, click Downloads, grab `mutiauk-linux-amd64`" replaces every other distribution channel.

## Step 7: Add Upstream Peers

A standalone agent on the shared host is useful for SOCKS-only ingress, but in practice you connect it to peer agents that act as exits for remote networks. Either edit `peers:` in the config and restart, or use Muti Metroo Manager's **Topology** tab to add peers at runtime.

How you wire up the rest of the mesh depends entirely on what you are trying to reach. The user manual's chapter on agent roles covers transit-vs-exit decisions; this manual stays focused on the operator-facing side from here on.

## Verifying the Shared Host Is Healthy

```bash
# Agent (no bearer header needed -- /health, /healthz, /ready are always public)
curl -s http://localhost:8080/healthz | jq

# Muti Metroo Manager (status page)
curl -s http://localhost:3000/

# Confirm the agent sees its peers (replace TOKEN with the cleartext bearer token)
curl -s -H "Authorization: Bearer TOKEN" \
  http://localhost:8080/api/dashboard | jq '.peers | length'
```

If `/healthz` returns `running: true` with `peer_count > 0`, and the dashboard URL loads in a browser, you are ready to onboard your first operator.
