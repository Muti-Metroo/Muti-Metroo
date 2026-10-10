# Onboarding a New Operator

This chapter walks through bringing a new team member online. Pre-conditions: the shared host is up (Chapter 3), Muti Metroo Manager is reachable, the **Downloads** tab is populated, and the new operator has a Linux workstation with sudo.

The full onboarding takes about ten minutes. At the end of it the operator has Mutiauk running as a systemd service, autoroutes flowing in from the shared agent, and `mutiauk status` reporting healthy.

## Step 1: Hand Over the URL and the Token

Send the operator two things:

- The Muti Metroo Manager URL (`https://muti-metroo.example.com`).
- The bearer token (or its location in your secrets manager).

That is everything they need. Privileged actions on agents (route changes, management commands, file transfer) are signed by the shared host's agent using the signing key it already has in its config; nothing private is ever pasted into the browser or the workstation.

## Step 2: Download Mutiauk from the Dashboard

Open the dashboard URL, paste the bearer token at the prompt, and the dashboard loads. Click the **Downloads** tab. Under "Mutiauk", pick the Linux architecture (a normal laptop is `amd64`; a Raspberry Pi or Apple Silicon VM is `arm64`).

```bash
# The Downloads tab in the UI shows a copy-paste curl command. Example for amd64:
curl -L -o mutiauk https://muti-metroo.example.com/api/downloads/file/mutiauk-linux-amd64

# Verify checksum (optional but recommended; SHA256 shown in the UI)
sha256sum mutiauk

chmod +x mutiauk
sudo mv mutiauk /usr/local/bin/
mutiauk version
```

If you do not have the **Downloads** tab (no bundle on the shared host), fall back to the project release page on GitHub -- the dashboard route is just a convenience.

## Step 3: Generate a Config from the Wizard

In Muti Metroo Manager, on the Downloads tab next to the Mutiauk section, click **Create config**. The Mutiauk config wizard opens with five steps:

1. **TUN interface.** Default name `mutiauk`, MTU 1400, address `10.200.200.1/24`. Accept the defaults unless the workstation already uses that subnet.
2. **SOCKS5 proxy.** The dashboard pre-fills the SOCKS5 endpoint of the shared host. Confirm it. Tick **Authentication** and paste the SOCKS5 username/password if your agent requires it.
3. **Routes.** Leave **autoroutes** on (the default). Routes and mesh hosts then flow to the workstation over the same SOCKS5 endpoint configured in the previous step -- there is no separate URL or token to enter here. Add any static routes you also want active before the first ones arrive.
4. **Admin interface.** The loopback-only, unauthenticated HTTP dashboard and JSON API for the daemon. Accept the defaults unless you need to change the bind address.
5. **Review & download.** Confirm the YAML preview, then click **Download**.

The wizard does not have a DNS-integration step. Mutiauk auto-detects the right backend at runtime; the default is fine.

Save the YAML and copy it onto the workstation:

```bash
sudo mkdir -p /etc/mutiauk
sudo mv ~/Downloads/config.yaml /etc/mutiauk/config.yaml
sudo chown root:root /etc/mutiauk/config.yaml
sudo chmod 600 /etc/mutiauk/config.yaml      # contains socks5.password
```

If you would rather skip the dashboard wizard and configure on the workstation, run `sudo mutiauk setup` -- it asks the same questions interactively and writes the same file.

## Step 4: Install the systemd Service

```bash
sudo mutiauk service install -c /etc/mutiauk/config.yaml
sudo systemctl status mutiauk
```

The installer creates `/etc/systemd/system/mutiauk.service`, enables it, and starts it. Tail the logs:

```bash
sudo journalctl -u mutiauk -f
```

You should see the daemon create the TUN interface, dial the SOCKS5 endpoint, receive routes over the SOCKS5 route-watch stream, and start the local DNS resolver.

The SOCKS5 password is read from the config file by the daemon, so there is no environment variable to set on the service unit. Day-to-day rotation means editing the config, `chmod 600`-ing it again, and `sudo systemctl restart mutiauk`.

## Step 5: Verify End-to-End

Run the built-in health check on the workstation:

```bash
sudo mutiauk status
```

You should see the daemon up, the TUN interface up, the SOCKS5 server reachable, the routes table populated with `auto`-source entries received over the SOCKS5 route-watch stream, and the connectivity tests (TCP routing, UDP routing) pass. If any line reports a failure, jump to Chapter 11 to debug.

Then try a real connection. Pick any CIDR that has a route in the mesh and a reachable host:

```bash
ping -c 3 10.20.120.11
curl --max-time 5 http://10.20.120.11/
```

If this works without setting any proxy environment variables, transparent routing is live.

## What the Operator Now Has

- `mutiauk` running as a service, restarting on reboot.
- Routes from the shared agent applied automatically; new CIDRs added in Muti Metroo Manager flow in within seconds (pushed over the SOCKS5 stream).
- Mesh-host names (`.mesh` etc.) resolve in any tool, not just SOCKS-aware ones, as long as a DNS integration backend was detected (see Chapter 9).
- A working `ping` for any routed destination.

What they do **not** have: anything that writes to the mesh from the workstation. Adding routes, adding mesh hosts, running management commands -- all of that is in Muti Metroo Manager.

## A Note on DNS

Mesh-host DNS works out of the box on most Debian-based installations. Mutiauk auto-detects the host resolver setup -- preferring `systemd-resolved` if present, otherwise NetworkManager's `dnsmasq` plugin -- and wires the suffix list automatically.

If your workstation has neither (a stripped-down Debian, or something unusual), `mutiauk dns status` will report `Backend: none` and mesh-host queries will not resolve via the system. The daemon's resolver is still reachable directly at the TUN address; you can also configure the backend explicitly. Chapter 9 covers both situations.

## Adding Yourself As a Peer (Optional)

The flow above assumes the operator is a _client_ of the shared mesh and never becomes a peer themselves. That is the recommended model -- it keeps the operator's machine off the mesh's flood/route propagation graph and avoids a maze of trust questions.

If you have a reason to make a workstation an actual mesh participant (long-running exit for a CIDR only that workstation can reach, for example), install `muti-metroo` on it instead of (or alongside) `mutiauk`. That setup is outside the scope of this chapter; see the user manual on agent roles.
