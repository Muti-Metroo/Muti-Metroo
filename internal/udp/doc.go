// Package udp provides UDP relay functionality for SOCKS5 UDP ASSOCIATE.
//
// This package implements the server-side (exit node) handling for UDP associations,
// including:
//   - Association lifecycle management (create, track, cleanup)
//   - UDP socket handling and datagram forwarding
//   - Integration with the mesh protocol for multi-hop relay
//   - E2E encryption for UDP datagrams
//
// UDP associations are created when a SOCKS5 client sends a UDP ASSOCIATE command.
// The association creates a UDP socket that relays datagrams between the SOCKS5
// client and the remote destination, with traffic encrypted through the mesh.
//
// # Lifecycle
//
//  1. Client sends SOCKS5 UDP ASSOCIATE to ingress agent
//  2. Ingress creates local UDP relay socket and sends UDP_OPEN through mesh
//  3. Exit agent receives UDP_OPEN, creates UDP socket, sends UDP_OPEN_ACK
//  4. Datagrams flow bidirectionally as UDP_DATAGRAM frames
//  5. Association closes when TCP control connection closes or times out
//
// # Thread Safety
//
// All exported methods are safe for concurrent use.
package udp
