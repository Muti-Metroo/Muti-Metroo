# A Tour of Muti Metroo Manager

Muti Metroo Manager is the operator's primary tool. Almost everything that is not running traffic happens here: adding routes, registering mesh hosts, running management commands, transferring a file, watching the topology.

This chapter is a tab-by-tab tour. The goal is that after reading it you can find any feature in the dashboard without hunting.

## Logging In

Open the dashboard URL in a browser. The first action that needs the bearer token (any privileged call: management commands, file transfer, route mutation) triggers a login modal:

1. Paste the bearer token from your password manager.
2. The token is stored in `sessionStorage` (browser tab only -- it is gone when you close the tab).
3. Subsequent privileged calls reuse it.

That is the only credential the operator handles. Privileged actions like running management commands or removing a route are signed by the **agent** on the shared host using its configured signing private key (see Chapter 3 Step 2). The operator never sees or pastes the signing key.

If you need to override the token Muti Metroo Manager uses to talk to the agent (for example, to authenticate as a service account separate from your own login), use the **Settings** dialog (the gear icon in the kebab menu).

## The Top Navigation

| Tab                 | What you do here                                                                                                                                 |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Topology**        | See every agent in the mesh, click to drill in. Default landing tab.                                                                             |
| **Routes**          | Add, remove, comment, and trace CIDR / domain routes.                                                                                            |
| **Port Forwarding** | Manage port-forward listeners and endpoints.                                                                                                     |
| **DNS**             | Mesh-host registry (`name.mesh -> ip`) and the suffix list (`.mesh`, `.corp.local`, ...).                                                        |
| **Downloads**       | Hand operators a single URL for `mutiauk`, `muti-metroo`, manuals. Only appears when a downloads bundle is loaded on the host (Chapter 3, Step 6). |

A kebab menu on the top-right exposes one-off actions: **Mesh Connectivity Test**, **Generate Muti Metroo Agent Config**, **Generate Mutiauk Config**, **Settings**, **Suspend Mesh** / **Resume Mesh** (the visible label flips with sleep state), and **Sign out**.

## Topology Tab

This is the tab operators stare at most. It is a metro-map style visualisation of the mesh:

- Each station is an agent, labelled with display name and short ID.
- Lines between stations are peer connections; thickness reflects traffic, colour reflects RTT (cool = healthy, warm = degraded), and an animated flow indicator marks active streams and their direction.
- Click a station to open the **agent panel** on the right.
- Drag stations to reposition; pan and zoom with mouse wheel. The view persists in the browser; **Reset view** restores defaults.
- The agent search collapses to a magnifying-glass icon by default. Press `/` anywhere to expand it; it filters by name, ID, role, IP, route, forward, or note.
- New agents pulse briefly when they come online; recent topology events scroll across a bottom ticker. Ambient sounds (boot / disconnect / mesh-test) can be muted from the in-app toggle, or globally with `muti-metroo-manager -no-sounds` (recommended in clinical, broadcast, and other noise-sensitive deployments).

![Mesh topology view](./01-topology.png)

The agent panel is itself a tabbed view. Each tab is a remote operation against the agent you clicked:

| Sub-tab          | What it does                                                                                                                                                                                                                                  |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Info**         | Display name, system info, uptime, role flags. Lets you set a free-text note and pick an icon (Linux/macOS/Windows/router/...). Notes and icons live in Muti Metroo Manager's local SQLite store, not on the agent.                                     |
| **Routes**       | Routes the selected agent advertises and learns.                                                                                                                                                                                              |
| **Forwards**     | Listeners and endpoints on this agent (with copy-paste `curl` examples).                                                                                                                                                                      |
| **Connectivity** | Peer connections and transport listeners, in **Peers** / **Listeners** sub-tabs. Peers lists direct connections from this agent; Listeners shows the transport sockets it binds and lets you add or remove them at runtime.                   |
| **Shell**        | Open an xterm.js terminal as the agent's user (requires `shell.enabled: true` on the target agent with the signing public key configured, and the signing private key configured on the agent Muti Metroo Manager talks to so it can sign the request). |
| **Files**        | Browse the agent's filesystem; upload, download, delete.                                                                                                                                                                                      |
| **Ping**         | Send ICMP echo from this agent to any IP, watch RTTs in real time.                                                                                                                                                                            |

![Agent Info tab with icon picker and notes](./03-agent-info.png)

Anything destructive (close peer, remove route, delete file) goes through a confirmation dialog.

## Routes Tab

Two sub-tabs:

**Active routes.** A flat table of every CIDR and domain route currently advertised across the mesh. Columns: destination, origin agent, hop count, metric, comment. Edit a comment inline; comments propagate over the wire so every agent that learns the route sees the same comment. The route edit dialog also sets an optional **Source IP** and egress **Interface** for the route's outbound dials -- both pickers are populated from the origin agent's interface addresses, and the dialog flags a mismatch when the chosen source IP lives on a different NIC than the pinned interface. These two bindings are local to the origin agent (they do not propagate). The `+` button adds a new route to a chosen origin agent; the trash icon removes one.

![Active routes table](./07-routes-table.png)

**Test routing.** Type a destination IP, CIDR, or hostname. Muti Metroo Manager posts to the agent's `/route/trace` endpoint and renders three labelled views: how a SOCKS client would route it, how a TUN client would route it, and how a domain-route lookup would resolve it. A collapsible "all candidates" section shows every CIDR considered with the reason each loser lost. Use this when an operator says "my traffic is going somewhere weird" -- Chapter 11 walks through that workflow.

![Test routing sub-tab with a populated trace](./13-routes-test-routing.png)

## Port Forwarding Tab

A single table of forward routes pairing **listeners** (ingress side, where remote clients connect) with **endpoints** (exit side, where the actual service lives). Unpaired listeners or endpoints stay visible so you can spot a dangling configuration. Click into the agent panel for a ready-to-paste `curl` example, including SNI variants for TLS-fronted backends.

![Port forwarding table with an unpaired listener](./08-port-forwarding.png)

## DNS Tab

**Hosts.** CRUD for the mesh-host registry. Add `target-11.mesh -> 10.8.0.11` with an optional comment, save, and every agent and every Mutiauk instance picks it up on the next dashboard poll. Config-seeded entries from the agent's YAML are visible but cannot be removed at runtime; edit the agent config and restart to drop those.

![DNS tab listing registered mesh hostnames](./12-domains-tab.png)

**TLDs.** The active suffix list. Default is just `.mesh`. Add `.corp.local`, remove `.mesh.alt`, whatever your team standardises on. Changes are live; nothing on the workstations needs to be touched manually. Single-label bare names without a configured suffix are rejected by the agent at validation time.

Mesh-host setup end-to-end is in Chapter 9.

## Downloads Tab

If a `muti-metroo-downloads-bundle.zip` is loaded, this tab serves the matching versions of every Muti Metroo and Mutiauk binary, the Windows DLL, and the user, operator, and slides PDFs. Each binary section has architecture tabs (`amd64` / `arm64`) and a SHA256 checksum. The Muti Metroo Manager binary itself is not delivered through this tab -- operators download it directly from the release page or from the URL where the dashboard is hosted.

Each binary section also has a **Create config** button that opens the matching config wizard inline. Use this when onboarding a new operator: download the binary, click Create config, fill in the wizard, save the YAML, hand both files over.

The Mutiauk config wizard has five steps: **TUN interface**, **SOCKS5 proxy**, **Routes**, **Admin interface**, **Review & download**. There is no DNS-integration step -- the workstation handles that automatically. Chapter 5 walks through the wizard for a fresh Linux workstation.

## What Muti Metroo Manager Does Not Do

A few things the dashboard explicitly does not handle:

- **Multi-tenant access.** The bearer token is the only credential. If an operator should not see the mesh, do not give them the token.
- **Multi-agent federation.** Muti Metroo Manager reverse-proxies one agent at a time. If you have two disjoint meshes, run two Muti Metroo Manager instances on different ports.

If you need any of those, your deployment has outgrown the assumed shape and you are off the path this manual covers.

## Useful Shortcuts

| Key   | Action                                                     |
| ----- | ---------------------------------------------------------- |
| `/`   | Focus the search box (jumps to the topology tab if needed) |
| `Esc` | Close the active agent panel                               |

URL hashes like `#/agents/<short-id>` open an agent panel directly -- handy for sharing a deep link in chat.
