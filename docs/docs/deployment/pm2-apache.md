---
title: PM2 with Apache
sidebar_position: 7
---

# PM2 with Apache

Run Muti Metroo under [PM2](/deployment/pm2) behind Apache's WebSocket reverse proxy using only a user directory and an `.htaccess` file. This fits shared hosting accounts and any host where you cannot install system services, edit the main Apache configuration, or bind privileged ports.

**When to use this:**

- Shared web hosting where Apache already terminates TLS for a domain you own
- A VPS or workstation where you do not have root but can run user processes and edit `.htaccess`
- You want to add Muti Metroo to an existing site without touching `sites-available/`

For full `VirtualHost`-level Apache, Nginx, or Caddy setups, see [Reverse Proxy Deployment](/deployment/reverse-proxy).

## Architecture

```mermaid
flowchart LR
    Remote[Remote Agent]
    Apache[Apache<br/>TLS termination<br/>.htaccess rewrite]
    Muti Metroo[Muti Metroo Agent<br/>plaintext ws<br/>on loopback]

    Remote -->|wss://example.com/mesh/| Apache
    Apache -->|ws://127.0.0.1:3001/mesh/| Muti Metroo
```

Apache handles TLS and the WebSocket upgrade. Muti Metroo listens in plaintext on loopback, reachable only from the same host.

## Prerequisites

- `mod_proxy`, `mod_proxy_http`, and `mod_proxy_wstunnel` enabled on the Apache server. Most shared hosts enable these by default; ask the provider if in doubt.
- Permission to place `.htaccess` files under the document root of the path you want to expose.
- PM2 available in the user environment (`npm install -g pm2`).

## Muti Metroo configuration

Configure a plaintext WebSocket listener bound to loopback. The listener `path` must match the URL prefix exposed by Apache.

```yaml
# config.yaml
agent:
  display_name: "Shared Host Agent"
  data_dir: "./data"

listeners:
  - transport: ws
    address: "127.0.0.1:3001"
    path: "/mesh/"
    plaintext: true

http:
  enabled: true
  address: "127.0.0.1:8080"
```

:::note Per-user loopback
Some shared hosts assign each account its own loopback range (for example `127.0.x.y`) and block `127.0.0.1`. If binding to `127.0.0.1` fails, use the address the provider documented, or that other tools on the account already bind to, and point both the Muti Metroo listener and the Apache `RewriteRule` at it.
:::

## .htaccess

Place an `.htaccess` file in the directory that serves the URL prefix (for example `public_html/mesh/`):

```apacheconf
RewriteEngine On

# Only proxy WebSocket upgrade requests. Regular HTTP requests fall
# through to whatever else lives at this prefix (static files, directory
# index, etc.) so they do not accidentally hit Muti Metroo.
RewriteCond %{HTTP:Connection} upgrade [NC]
RewriteCond %{HTTP:Upgrade} websocket [NC]

# Preserve the full path and query string when forwarding to Muti Metroo.
RewriteRule ^(.*)$ ws://127.0.0.1:3001/mesh/$1 [P,L]
```

Key points:

- The `RewriteRule` target uses `ws://` (not `http://`) so `mod_proxy_wstunnel` handles the upgrade cleanly.
- The two `RewriteCond` lines ensure only actual WebSocket upgrades are proxied; plain GETs are unaffected.
- The path passed to Muti Metroo (`/mesh/`) must match the `path` in the listener configuration.

## PM2 manifest

Define the process in a manifest file under your home directory:

```json
// ~/muti-metroo.json
{
  "apps": [
    {
      "name": "muti-metroo",
      "script": "./muti-metroo",
      "args": "run -c ./config.yaml",
      "cwd": "./public_html/mesh",
      "interpreter": "none",
      "autorestart": true,
      "max_restarts": 10,
      "restart_delay": 5000
    }
  ]
}
```

`interpreter: "none"` tells PM2 to exec the binary directly instead of treating it as a Node.js script.

## Lifecycle

```bash
# Start from the manifest
pm2 start ~/muti-metroo.json
pm2 save

# Enable startup at boot (run the command PM2 prints)
pm2 startup
```

Update the binary in place:

```bash
# PM2 keeps a handle on the executable - stop first, then swap.
pm2 stop muti-metroo
cp /tmp/muti-metroo-new ./muti-metroo && chmod +x ./muti-metroo
pm2 start muti-metroo
```

Logs live under `~/.pm2/logs/muti-metroo-out.log` and `~/.pm2/logs/muti-metroo-error.log` by default. See [PM2 Process Manager](/deployment/pm2) for ecosystem-file options, log rotation, and Windows notes.

## Peering from other agents

Remote agents dial the public `wss://` URL. Use the agent's full 128-bit ID (from its `data_dir/agent_id`) so the handshake verifies identity.

```yaml
# Remote agent peers block
peers:
  - id: "f49038d87149b6c548ef6d8192a59106"
    transport: ws
    address: "wss://example.com/mesh/"
```

## Notes

- TLS terminates at Apache; the Muti Metroo hop is plaintext on loopback. End-to-end encryption (X25519 + ChaCha20-Poly1305) still protects stream payloads, so the proxy and any transit nodes cannot read them.
- mTLS (client certificates) is unavailable in this mode because TLS is not handled by Muti Metroo. Peer identity is still validated by the PEER_HELLO handshake.
- One `.htaccess` rule serves a single path. If you need several Muti Metroo listeners on the same domain, use distinct paths (`/mesh-a/`, `/mesh-b/`) and a matching listener and rewrite rule for each.

## Related

- [PM2 Process Manager](/deployment/pm2)
- [Reverse Proxy Deployment](/deployment/reverse-proxy)
- [Configuration - Listeners](/configuration/listeners)
