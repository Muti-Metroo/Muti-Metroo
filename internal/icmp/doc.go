// Package icmp provides ICMP echo (ping) support for exit nodes.
//
// The package enables sending and receiving ICMP echo requests/replies through
// the mesh network with full E2E encryption (X25519 + ChaCha20-Poly1305).
//
// # Architecture
//
// ICMP sessions work similarly to UDP associations:
//
//  1. Client sends ICMP_OPEN with destination IP and ephemeral public key
//  2. Exit node creates unprivileged ICMP socket and performs key exchange
//  3. Exit node sends ICMP_OPEN_ACK with its ephemeral public key
//  4. Both sides derive shared session key via ECDH
//  5. Client sends ICMP_ECHO frames with encrypted payload
//  6. Exit node decrypts, sends real ICMP ping, receives reply
//  7. Exit node encrypts reply and sends ICMP_ECHO back to client
//  8. ICMP_CLOSE terminates the session
//
// # Unprivileged ICMP Sockets
//
// On Linux, unprivileged ICMP requires the ping_group_range sysctl:
//
//	sysctl -w net.ipv4.ping_group_range="0 65535"
//
// This allows non-root users to send ICMP echo requests.
//
// # Configuration
//
// ICMP is enabled by default. To disable or customize:
//
//	icmp:
//	  enabled: true
//	  max_sessions: 100
//	  idle_timeout: 60s
//	  echo_timeout: 5s
//
// # Security
//
// All echo payloads are encrypted with ChaCha20-Poly1305. Transit nodes cannot
// decrypt the payload; they only relay encrypted ICMP_ECHO frames.
package icmp
