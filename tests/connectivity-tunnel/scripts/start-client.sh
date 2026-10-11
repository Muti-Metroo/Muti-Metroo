#!/bin/bash
# Start Muti Metroo ingress and Mutiauk TUN daemon in the client container
set -e

echo "=== Starting Muti Metroo ingress agent ==="
muti-metroo run -c /etc/muti-metroo/config.yaml &
MM_PID=$!
echo "Muti Metroo started with PID $MM_PID"

# Wait for SOCKS5 to be ready
echo "Waiting for SOCKS5 proxy to be ready..."
for i in {1..30}; do
    if nc -z 127.0.0.1 1080 2>/dev/null; then
        echo "SOCKS5 proxy is ready"
        break
    fi
    sleep 1
done

# Wait for routes to propagate
echo "Waiting for routes to propagate (10s)..."
sleep 10

echo "=== Starting Mutiauk TUN daemon ==="
mutiauk daemon start -c /etc/mutiauk/config.yaml &
MUTIAUK_PID=$!
echo "Mutiauk started with PID $MUTIAUK_PID"

# Wait for TUN interface
echo "Waiting for TUN interface..."
for i in {1..30}; do
    if ip link show tun0 2>/dev/null; then
        echo "TUN interface is ready"
        break
    fi
    sleep 1
done

echo ""
echo "=== Setup complete ==="
echo "TUN interface:"
ip addr show tun0 2>/dev/null || echo "TUN interface not found"
echo ""
echo "Route to target:"
ip route get 192.168.107.100 2>/dev/null || echo "No route to 192.168.107.100"
echo ""
echo "You can now test connectivity:"
echo "  nc -zv 192.168.107.100 80"
echo "  curl http://192.168.107.100/"
echo ""
echo "Target services:"
echo "  TCP: 22 (SSH), 80 (HTTP), 443 (HTTPS)"
echo "  UDP: 53 (DNS), 123 (NTP)"
echo ""

# Keep running and drop to shell
exec /bin/bash
