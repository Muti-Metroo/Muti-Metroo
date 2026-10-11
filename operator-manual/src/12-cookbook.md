# Cookbook

Short recipes for situations that come up often. Each one is a few lines -- enough to remind you what to do without re-reading a chapter.

## Add a Temporary CIDR for One Project

In Muti Metroo Manager -> **Routes** -> **Active routes** -> **Add route**:

- Network: the project CIDR
- Origin: the agent that exits for it
- Comment: `project-Q2-26` (and the date)

When done, click the trash icon. The route is gone everywhere within seconds. No workstation cleanup needed.

## Pull a File Off a Target Box

Two ways, depending on what is reachable.

**The target has SSH and the route is up:**

```bash
scp op@target-11.mesh:/data/file.tar.gz ./
```

**No SSH on the target, but the agent is reachable via Muti Metroo Manager:**

In the agent panel, **Files** sub-tab. Browse to the file, click download. The file streams over the WebSocket, signed with the management key.

## Share a Tool With a Target Box You Are Working On

```bash
mutiauk expose add folder tools /home/op/tools --comment "project"
# In Muti Metroo Manager -> Port Forwarding, add a listener for key "tools" on a mesh agent
# the target box can reach. Then on the target box:
#   curl -O http://that-agent:8090/diagnostics.sh
# When done:
mutiauk expose remove tools
```

## Why Does ping Work but TCP Does Not?

Most likely: ICMP is permissive on the destination but TCP is firewalled. Try `nc -zv target 22` to a port you expect to be open. If that fails, the destination's firewall is the problem, not the mesh.

Less likely but worth checking: the exit agent has ICMP enabled but its outbound TCP is restricted. Check the exit agent's logs and config.

## I Changed Routes on the Agent and the Workstation Still Uses the Old Ones

Give it a few seconds (updates are pushed over the SOCKS5 stream). Then:

```bash
mutiauk route list
```

If it still shows the old route, kick the daemon:

```bash
sudo systemctl restart mutiauk
```

If the route is gone after restart but comes back within seconds, the agent is still advertising it -- the dashboard change did not take effect. Reload the dashboard, double-check the **Active routes** tab.

## Reset Mutiauk Without Rebooting

```bash
sudo systemctl stop mutiauk
sudo mutiauk cleanup
sudo systemctl start mutiauk
sudo mutiauk status
```

`cleanup` reverts the TUN interface, the daemon socket, and the DNS integration. Add `--purge` if you also want to clear persisted routes. Safe to run on a working setup; it is a no-op when nothing needs cleaning.

## Force a SOCKS Application Through the Mesh Even Though TUN Is Active

If the application speaks SOCKS5, point it at `127.0.0.1:1080` (Mutiauk's local SOCKS5 listener) or directly at the shared agent's SOCKS5. Tools that know about SOCKS5 use that path; the TUN does not see those packets.

```bash
curl -x socks5h://127.0.0.1:1080 https://internal.example/
```

## Check Service Reachability Through the Mesh

```bash
curl --max-time 5 http://10.20.120.11/
ssh op@10.20.120.11
nc -zv 10.20.120.11 22
```

TCP-connect-based checks flow through the mesh; raw-socket tools bypass the TUN.

## Add a Mesh Hostname Quickly

In Muti Metroo Manager -> **DNS** -> **Hosts** -> **Add host**:

- Name: `target.mesh`
- IPv4: `10.x.y.z`
- Comment: a hint about what it is

Save. Within seconds on each workstation, `ssh op@target.mesh` works.

## Run Management Commands to Diagnose Something

In Muti Metroo Manager -> **Topology** -> click the agent -> **Shell** sub-tab. You get an xterm.js terminal as the agent's user. Run `ps`, `journalctl`, `ip route`, whatever you need. Authorisation is handled by the shared host's agent signing the request with its configured signing private key; nothing is pasted into the browser.

If the tab errors with "shell not enabled", the target agent's config has `shell.enabled: false` (the default). Edit, restart, retry. If it errors with "missing signing key", the shared host's agent is missing `management.signing_private_key` (Chapter 3 Step 2).

## Restart the Mesh-Wide State of Things

This is rarely the right answer, but when it is:

```bash
# On the shared host
sudo systemctl restart muti-metroo
sudo systemctl restart muti-metroo-manager
```

Workstations reconnect within seconds. Routes re-propagate within one advertise interval (default 2 minutes).

## Rotate the Bearer Token

On the shared host, pick a new cleartext token (`openssl rand -hex 32` is one easy source), pipe it through `muti-metroo hash` to bcrypt it, paste the resulting hash into `http.token_hash` in the agent config, and restart the agent:

```bash
openssl rand -hex 32 | tee /root/new-token.txt | muti-metroo hash
# paste the resulting bcrypt hash into http.token_hash in the agent config
sudo systemctl restart muti-metroo
```

Distribute the new cleartext value to operators -- it is what they paste into Muti Metroo Manager to log in. Workstations need no update: Mutiauk does not carry this token (autoroutes rides the SOCKS5 endpoint and is unaffected). If you also rotate SOCKS5 credentials, that is a separate change -- update `socks5.password` on each workstation, `chmod 600` the config, and `sudo systemctl restart mutiauk`.

## Confirm the Mesh Is Healthy in 30 Seconds

```bash
# Workstation health
sudo mutiauk status

# Mesh-wide health from the dashboard kebab menu
# (or via the API):
curl -s -H "Authorization: Bearer $MUTI_METROO_TOKEN" \
     http://localhost:8080/api/mesh-test | jq '{total_count, reachable_count, duration_ms}'
```

The dashboard's **Mesh Connectivity Test** (kebab menu) runs the same probes, displayed in a modal with per-pair results.
