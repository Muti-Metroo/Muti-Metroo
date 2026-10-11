# Debugging Routing and DNS

When something does not work the way you expect, this chapter is the bisection guide. It is split into routing problems and DNS problems, each with a top-down flow that starts cheap and gets more expensive only if needed.

The two commands you reach for most often are:

```bash
mutiauk route trace <ip-or-hostname>
sudo mutiauk dns status
```

Both have a `--json` variant for scripting; the human output is good for quick diagnosis.

## Routing Problems

### Symptom: a destination you expect to reach times out

**Step 1. Did the route apply?**

```bash
mutiauk route trace 10.20.120.11

Destination:  10.20.120.11
Match:        10.20.0.0/16
Interface:    tun0 (Mutiauk)
Mesh Path:    shared-host -> peer-east -> exit-dc-a
Origin:       exit-dc-a [76e822ad]
Hop Count:    2
```

This is the happy case -- the workstation has a route, the route is via `tun0`, and the daemon believes there is a reachable mesh path. If the destination still times out, the problem is past the exit (the destination box is down, or the exit cannot reach it). Try another destination in the same CIDR.

If you instead see:

```
Match:        0.0.0.0/0 via 192.168.1.1
Interface:    eth0
Note:         Resolved IP is not in any mutiauk route; kernel will
              send it via eth0. Add a static route or autoroute to
              forward it through the mesh.
```

The CIDR is not on the workstation. Either it never appeared (autoroutes broken; see below) or it was filtered out (overlaps with a local subnet, or marked unsafe). `mutiauk route list` shows what made it through.

**Step 2. If the route is missing, is autoroutes working?**

```bash
sudo journalctl -u mutiauk -n 100 --no-pager | grep -iE 'autoroutes|route-watch|socks'
```

Common output:

- `autoroutes: applied N routes` -- working, route just was not advertised yet (it arrives over the SOCKS5 route-watch stream within seconds of being added).
- `autoroutes: route-watch unavailable` / no snapshot -- the workstation cannot open the route-watch stream. Check `socks5.server` reachability and the SOCKS5 credentials (autoroutes rides the SOCKS5 endpoint; there is no separate URL or bearer token).
- `autoroutes: dial tcp .. connection refused` -- the workstation cannot reach the shared agent's SOCKS5 endpoint (`socks5.server`). Check network reachability and firewall rules.
- Nothing at all -- autoroutes is disabled. Check `autoroutes.enabled: true` in `/etc/mutiauk/config.yaml`.

**Step 3. If the route is on the workstation but the trace says "not in mesh", the agent side is wrong.**

In Muti Metroo Manager, **Routes** -> **Active routes**, search for the CIDR. If it is missing, no agent is advertising it; add it (Chapter 6) or fix the agent config that should be advertising it.

If it is in the table but the trace still says no mesh path, the origin agent is offline. **Topology** tab will show it greyed out. Restart the agent or talk to whoever runs it.

**Step 4. Confirm from the agent's perspective.**

The dashboard's **Test routing** sub-tab on the **Routes** tab runs the same trace from the shared host's agent. Compare its mesh path with the workstation trace. Any divergence isolates whether the issue is "agent does not know the path" or "workstation does not know the path".

![Test routing sub-tab](./13-routes-test-routing.png)

The CLI equivalent, useful when scripting:

```bash
# Substitute the cleartext bearer token for TOKEN.
# /route/trace is a read-only diagnostic; no signing key required.
curl -s -H "Authorization: Bearer TOKEN" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/route/trace \
     -d '{"dest": "10.20.120.11"}' | jq
```

### Symptom: ping works but TCP does not (or vice versa)

The cleanest signal: pull up `mutiauk status`. It runs separate TCP and UDP probes through the TUN. If TCP is OK and UDP fails, your problem is UDP-specific (look at the UDP relay configuration on the exit). If both probes pass but a real connection still fails, the destination port is closed or filtered downstream of the exit.

If ping works but every TCP attempt times out, the most common cause is that the exit agent's outbound is restricted -- it can echo, but cannot make new TCP connections. Open the exit's agent panel in Muti Metroo Manager and check **Routes** for what it is configured to exit, and **Info** for any errors logged.

### Symptom: routes appear but disappear after a few minutes

Routes have a TTL (default 5 minutes). The origin agent must re-advertise within the TTL. If routes flap, the origin agent is restarting, its peer connection is unstable, or the advertise interval has been pushed too high. Look at the origin agent's logs.

## DNS Problems

### Symptom: a mesh-host name returns NXDOMAIN

**Step 1. Confirm the resolver is reachable and answering.**

```bash
sudo mutiauk dns status
```

You want:

- A non-`none` backend (`resolvectl` or `nm-dnsmasq`).
- Your suffix in the **Active TLDs** line.
- The hostname in `mutiauk host list`.
- Both probes (`resolver direct` and `system stub`) showing `[OK] ... -> NXDOMAIN`.

If any of those is wrong, the problem is upstream:

- No backend -> Mutiauk could not auto-detect a supported host resolver (`systemd-resolved` or NetworkManager-with-dnsmasq). Chapter 9 covers what to do; in many cases querying the daemon's resolver directly (`dig @<tun-ip> ...`) is enough.
- TLD missing -> the dashboard does not have your suffix active, or the route-watch snapshot has not reached the workstation yet (it normally arrives within seconds over the SOCKS5 stream).
- Hostname not in `host list` -> dashboard does not have the entry, or the route-watch snapshot has not reached the workstation yet.

**Step 2. Confirm the host actually queries the resolver.**

```bash
resolvectl query target-11.mesh
```

If `resolvectl` reports `target-11.mesh: 10.8.0.11 -- link: tun0` you are done. If it returns `not found`, your suffix is not configured on the link, which means Mutiauk did not wire the integration correctly. `dns status` should have already flagged this.

**Step 3. Different tools, different paths.**

- `getent hosts foo` -- uses NSS, which goes through whichever resolver the host is configured with -- works whenever the integration is wired.
- `host foo` and `nslookup foo` -- talk straight to whatever is in `/etc/resolv.conf`. Whether they work depends on what owns that file.
- `dig @<resolver-ip> foo` -- explicit resolver. Useful for bisection.
- `dig @<tun-ip> foo` -- straight to Mutiauk's in-process resolver, bypasses all host wiring. Always works when the daemon is up.

If `dig @<tun-ip> target-11.mesh` returns the right answer but `getent hosts target-11.mesh` does not, the resolver is fine and the host integration is broken. Re-run `mutiauk dns status`, fix the backend, restart the daemon.

### Symptom: a single-label name was rejected

The agent rejects single-label bare names like `mesh` (no leading label) at validation time. If a host registration fails with "name is single-label; register a multi-label name", use a dotted form (`target.mesh`, or just any name with at least one `.`).

## When to Use --json Output

For interactive debugging, the human output is fine. For ticket templates, automation, or comparing two workstations:

```bash
mutiauk status --json --skip-tests | jq
mutiauk route trace 10.20.120.11 --json | jq
sudo mutiauk dns status --json | jq
```

`route trace --json` includes a `mesh_path_status` field with values:

| Status | Meaning |
|--------|---------|
| `ok` | A mesh path exists and the route is via Mutiauk. |
| `unreachable` | The CIDR is on the workstation, but the origin agent is offline. |
| `not_in_mesh` | The CIDR is not on the workstation; would not go through the mesh. |
| `not_configured` | Mutiauk is not running, or autoroutes is off. |

That field is the single most useful thing for "is this user error, configuration error, or a mesh outage" triage.

## Last Resort: Cleanup

If the daemon got into a weird state -- crashed during a route apply, left a stale TUN interface, broke DNS wiring -- reset the host:

```bash
sudo systemctl stop mutiauk
sudo mutiauk cleanup
```

This removes the TUN interface, the stale PID file, the daemon socket, and reverts the DNS integration. It does not touch the config or the persisted routes file unless you also pass `--purge`.

```bash
sudo mutiauk cleanup --purge   # Also removes /var/lib/mutiauk/routes.json
```

Restart the daemon after cleanup:

```bash
sudo systemctl start mutiauk
sudo mutiauk status
```

If the cleanup itself reports the daemon is still active, `systemctl stop` did not finish; wait, then re-run.
