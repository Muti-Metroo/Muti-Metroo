# Muti Metroo Docker Tryout

A ready-to-run 4-agent mesh example demonstrating SOCKS5 proxy, split exit routing, management commands, and file transfer.

## Topology

```
                +-------------+
                |   Agent 1   |
                |  (Ingress)  |
                | SOCKS5:1080 |
                | HTTP API    |
                +------+------+
                       |
                +------v------+
                |   Agent 2   |
                |  (Transit)  |
                |Shell + Files|
                +------+------+
                       |
          +------------+------------+
          |                         |
   +------v------+           +------v------+
   |   Agent 3   |           |   Agent 4   |
   |   (Exit)    |           |   (Exit)    |
   | 0.0.0.0/1   |           | 128.0.0.0/1 |
   |Shell + Files|           |Shell + Files|
   +-------------+           +-------------+
```

- **Agent 1** (Alpine): SOCKS5 ingress + HTTP API
- **Agent 2** (Ubuntu): Transit node with shell and file transfer
- **Agent 3** (Ubuntu): Exit for IPs 0.0.0.0 - 127.255.255.255
- **Agent 4** (Ubuntu): Exit for IPs 128.0.0.0 - 255.255.255.255

## Quick Start

```bash
# Start the mesh
docker compose up -d

# View logs
docker compose logs -f

# Stop
docker compose down
```

## Access Points

- **SOCKS5 Proxy**: `localhost:11080`
- **HTTP API**: `http://localhost:18080/api/dashboard`

## Usage Examples

### SOCKS5 Proxy

Route traffic through the mesh:

```bash
# HTTP request via SOCKS5
curl -x socks5h://localhost:11080 https://httpbin.org/ip
curl -x socks5h://localhost:11080 https://ifconfig.me
```

### SSH Through Tunnel

```bash
# SSH to a remote host via the SOCKS5 proxy
ssh -o ProxyCommand='nc -x localhost:11080 %h %p' user@remote-host

# Or with OpenSSH 7.6+ built-in SOCKS support
ssh -o 'ProxyCommand=nc -X 5 -x localhost:11080 %h %p' user@remote-host
```

### Management Commands

Run management commands on remote agents (requires muti-metroo CLI on host):

```bash
# Agent IDs are fixed in this example:
# - agent2: bbbb222222222222bbbb222222222222
# - agent3: cccc333333333333cccc333333333333
# - agent4: dddd444444444444dddd444444444444

# Run a command on agent2
muti-metroo shell -a localhost:18080 bbbb222222222222bbbb222222222222 whoami

# Check OS on agent4 (Ubuntu)
muti-metroo shell -a localhost:18080 dddd444444444444dddd444444444444 cat /etc/os-release

# Interactive bash on agent4
muti-metroo shell -a localhost:18080 --tty dddd444444444444dddd444444444444 bash
```

### File Transfer

Upload and download files to/from remote agents:

```bash
# Upload a file to agent3
echo "Hello from host" > /tmp/test.txt
muti-metroo upload -a localhost:18080 cccc333333333333cccc333333333333 /tmp/test.txt /tmp/uploaded.txt

# Download a file from agent3
muti-metroo download -a localhost:18080 cccc333333333333cccc333333333333 /tmp/uploaded.txt /tmp/downloaded.txt
cat /tmp/downloaded.txt

# Download system file
muti-metroo download -a localhost:18080 cccc333333333333cccc333333333333 /etc/hostname ./agent3-hostname.txt
```

### View Mesh Status

```bash
# List all agents
curl -s http://localhost:18080/api/topology | jq '.agents[] | {id, name: .display_name}'

# Check health
curl -s http://localhost:18080/healthz | jq

# Dashboard data (JSON)
curl -s http://localhost:18080/api/dashboard | jq
```

## Troubleshooting

### Check agent logs

```bash
docker compose logs agent1
docker compose logs agent2
docker compose logs agent3
docker compose logs agent4
```

### Verify agents are connected

```bash
# Should show 4 agents
curl -s http://localhost:18080/api/topology | jq '.agents | length'
```

### Rebuild after changes

```bash
docker compose down
docker compose build --no-cache
docker compose up -d
```
