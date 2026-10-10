#!/bin/bash
# Verify routing through TUN interface
set -e

TARGET_IP="192.168.107.100"

echo "=== Checking TUN interface ==="
if ip addr show tun0 2>/dev/null; then
    ip addr show tun0
else
    echo "ERROR: TUN interface not found"
    echo "Run /scripts/start-client.sh first"
    exit 1
fi

echo ""
echo "=== Checking route table ==="
echo "Route to target ($TARGET_IP):"
ip route get $TARGET_IP || echo "No route"

echo ""
echo "=== Testing TCP connectivity through tunnel ==="
for port in 22 80 443; do
    echo "Testing target:$port..."
    if timeout 10 nc -zv $TARGET_IP $port 2>&1; then
        echo "SUCCESS: target:$port reachable"
    else
        echo "FAILED: target:$port not reachable"
    fi
done

echo ""
echo "=== Testing HTTP through tunnel ==="
if curl -s --max-time 10 http://$TARGET_IP/ | grep -q OK; then
    echo "SUCCESS: HTTP request returned OK"
else
    echo "FAILED: HTTP request through tunnel"
fi

echo ""
echo "=== Testing UDP DNS through tunnel ==="
if dig +time=5 +tries=1 @$TARGET_IP test.example | grep -q "127.0.0.1"; then
    echo "SUCCESS: DNS query answered through tunnel"
else
    echo "FAILED: no DNS answer through tunnel"
fi

echo ""
echo "=== Testing UDP echo responder through tunnel ==="
if echo ping | timeout 5 nc -u -w2 $TARGET_IP 123 | grep -q "NTP OK"; then
    echo "SUCCESS: UDP 123 responder replied"
else
    echo "FAILED: no reply from UDP 123 responder"
fi
