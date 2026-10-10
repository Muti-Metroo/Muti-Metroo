# Use Case: Exposing Folders and Ports from a Workstation

Operators routinely need to make something on their workstation reachable from the deep mesh. The classic case: you are working on a target box reached via the mesh, you need a tool that is on your laptop, and the target cannot reach your laptop directly. With Mutiauk's `expose` feature, the target can pull the tool through the nearest agent, transparently, without you ever publishing your laptop to the public internet.

The model is simple: you tell the local daemon "publish this folder (or this TCP target) on the mesh under name X", the daemon registers it over its SOCKS5 connection to the shared agent (a Muti Metroo protocol extension on the same SOCKS5 endpoint it uses for traffic), and any agent that accepts forward listeners can route a request for X back to your workstation. When you remove the entry (or your daemon disconnects), the registration disappears.

This works whenever `socks5.server` is set in the workstation config -- expose runs over the SOCKS5 endpoint and is independent of `autoroutes`.

## Two Types of Forwards

| Type     | What you publish                              | Behaviour                                                                                                                                     |
| -------- | --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `folder` | A directory on disk                           | Strict static file server. Returns 404 for any path that does not resolve to a regular file. No directory listings. No `index.html` fallback. |
| `tcp`    | A `host:port` reachable from your workstation | Plain TCP relay; pipes bytes both ways.                                                                                                       |

Both register under a **routing key** -- a short identifier (`[A-Za-z0-9._-]+`) that consumers use to address the forward.

## Sharing a Tools Folder With the Mesh

You are working on a target box deep in the mesh and you need `diagnostics.sh`, a config template, a custom script, or the ten other things every operator keeps in `~/tools`. The target cannot reach you, but it _can_ reach the nearest mesh agent.

On your Linux workstation:

```bash
mutiauk expose add folder tools /home/op/tools \
    --comment "operator toolkit"

mutiauk expose status
Expose: connected since 12:34:55
KEY    TYPE    SOURCE   TARGET/PATH       STATUS  STREAMS
tools  folder  runtime  /home/op/tools    [OK]    0
```

The daemon does not need root for this -- it talks to its own daemon over a Unix socket. Drop `sudo` from the command unless your toolkit lives somewhere that needs elevated read access.

In Muti Metroo Manager -> **Port Forwarding**, somebody (you or another operator with dashboard access) adds a forward listener on the agent that the target machine can reach:

| Field           | Value                                        |
| --------------- | -------------------------------------------- |
| Listener agent  | `exit-dc-a` (the agent the target can reach) |
| Key             | `tools`                                      |
| Address         | `:8090`                                      |
| Max connections | 50                                           |

The listener and your endpoint pair up by key. From the target box:

```bash
# On the target, reaching back through the nearest mesh agent:
curl -O http://exit-dc-a:8090/diagnostics.sh
curl -O http://exit-dc-a:8090/setup-7.bin
```

The HTTP request lands on `exit-dc-a:8090`, traverses the mesh to the shared agent, hops over the SOCKS5 expose stream into Mutiauk on your laptop, and is served by the strict file server out of `/home/op/tools`. From the target's perspective the file is coming from a perfectly normal local mesh agent.

When you are done:

```bash
mutiauk expose remove tools
```

The registration vanishes. If you also stop the daemon, the SOCKS5 connection closes and the forward goes away even if you forgot to remove it explicitly.

## Sharing a TCP Service

Same pattern, different type. You want a teammate (or a script on a target box) to reach a service running on your laptop -- a custom HTTP listener you wrote, a Python REPL on a port, anything that speaks plain TCP:

```bash
mutiauk expose add tcp my-listener 127.0.0.1:9999 \
    --comment "interactive listener for the project"
```

A listener with key `my-listener` on the chosen mesh agent forwards any incoming TCP connection to your `127.0.0.1:9999`. Pipe bytes, do whatever you need.

Anything plain TCP works: a custom listener, a Postgres tunnel, an stunnel-fronted SMB share. The relay does not care about the protocol.

## Why Forwards Are "Runtime-Only" by Default

`expose add` registers the forward with the running daemon and the upstream agent. It does not write to `/etc/mutiauk/config.yaml`. If you stop the daemon (or it crashes, or the workstation reboots), the registration is gone.

This is intentional: most exposed services are short-lived (an hour, a task), and you do not want stale entries advertising themselves after a reboot.

If you do want a forward to come back after a restart, edit the workstation config:

```yaml
expose:
  enabled: true
  forwards:
    - key: tools
      type: folder
      path: /srv/tools
      comment: "team tools share"
```

`SIGHUP` (or `mutiauk daemon reload`) picks up changes without a full restart. Config-defined keys are protected from runtime mutation -- you cannot `expose remove tools` on a key that came from the YAML.

## Listing What Is Exposed Mesh-Wide

`expose status` only shows your local registrations. To see everyone else's, open Muti Metroo Manager's **Port Forwarding** tab -- expose-registered forwards appear alongside ordinary forward endpoints. They are labelled with the originating agent (which, for an exposed forward from your workstation, is the shared agent that you are connected through).

![Port Forwarding tab showing the listener / endpoint pair](./08-port-forwarding.png)

## Security Considerations

A few things to keep in mind:

- **A folder share is a static file server, but it is on the mesh.** Anyone with a forward listener pointing at the right key can read it. Do not point it at `/home/op` or `/`.
- **TCP shares relay opaque bytes.** If the service requires authentication, the expose relay does not add any -- it just gets the bytes through. Make sure the service has its own auth.
- **SOCKS5 credentials gate the expose stream.** Expose authenticates with the SOCKS5 username/password (or none, if the proxy is open) -- not the autoroutes bearer token. If `socks5.server` is unreachable or the SOCKS5 credentials are wrong, the daemon cannot register forwards and `expose status` reports the connection as `disconnected`.
- **Routing keys are namespaced per agent.** Two operators registering `tools` from different workstations both succeed; the listener side picks one of them based on routing decisions. If you want to be sure you are reaching a specific operator's box, use distinct keys.

## Cleaning Up After Yourself

At the end of a session:

```bash
# List everything you have
mutiauk expose list

# Remove everything you do not need
mutiauk expose remove tools
mutiauk expose remove my-listener

# Or just stop the daemon -- forwards are registered through its SOCKS5 connection
sudo systemctl stop mutiauk
```

Restart later when you are ready; whatever was in `/etc/mutiauk/config.yaml` comes back, and runtime entries do not.
