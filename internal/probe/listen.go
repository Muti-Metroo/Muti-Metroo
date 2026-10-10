// Package probe provides connectivity testing for Muti Metroo listeners.
package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/transport"
)

// ListenOptions contains configuration for a probe listener.
type ListenOptions struct {
	// Transport type: "quic", "h2", "ws"
	Transport string

	// Address is the listen address (e.g., "0.0.0.0:4433")
	Address string

	// Path is the HTTP path for h2/ws transports (default: "/mesh")
	Path string

	// TLSCert is the path to the TLS certificate file
	TLSCert string

	// TLSKey is the path to the TLS private key file
	TLSKey string

	// TLSCA is the optional path to CA certificate for client verification (mTLS)
	TLSCA string

	// PlainText enables plaintext mode (no TLS) for WebSocket transport.
	// Use this when the listener is behind a reverse proxy that handles TLS termination.
	// Only valid for WebSocket transport.
	PlainText bool

	// DisplayName is the name shown in handshake responses
	DisplayName string

	// Protocol identifier overrides
	ALPNProtocol  string
	HTTPHeader    string
	WSSubprotocol string
}

// ConnectionEvent represents a connection attempt result.
type ConnectionEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	RemoteAddr string    `json:"remote_addr"`
	RemoteID   string    `json:"remote_id,omitempty"`
	RemoteName string    `json:"remote_name,omitempty"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	RTTMs      float64   `json:"rtt_ms,omitempty"`
}

// Listen starts a probe listener that accepts connections and responds to handshakes.
// It sends connection events to the provided channel.
// The listener stops when the context is cancelled.
func Listen(ctx context.Context, opts ListenOptions, eventChan chan<- ConnectionEvent) error {
	// Set defaults
	if opts.Path == "" {
		opts.Path = "/mesh"
	}
	if opts.DisplayName == "" {
		opts.DisplayName = "probe-listener"
	}

	// Validate plaintext option
	if opts.PlainText && opts.Transport != "ws" {
		return fmt.Errorf("plaintext mode is only supported for WebSocket transport")
	}

	// Load TLS configuration (nil for plaintext mode)
	var tlsConfig *tls.Config
	var err error
	if !opts.PlainText {
		tlsConfig, err = buildListenerTLSConfig(opts)
		if err != nil {
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
	}

	// Create transport
	tr, err := createTransport(opts.Transport)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}
	defer tr.Close()

	// Build listen options
	listenOpts := transport.ListenOptions{
		TLSConfig:     tlsConfig,
		Path:          opts.Path,
		PlainText:     opts.PlainText,
		ALPNProtocol:  opts.ALPNProtocol,
		HTTPHeader:    opts.HTTPHeader,
		WSSubprotocol: opts.WSSubprotocol,
	}

	// Start listener
	listener, err := tr.Listen(opts.Address, listenOpts)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	defer listener.Close()

	// Accept loop
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Accept connection with timeout to allow context checking
		acceptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := listener.Accept(acceptCtx)
		cancel()

		if err != nil {
			// Check if context was cancelled
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Timeout is normal, continue accepting
			continue
		}

		// Handle connection in goroutine
		go func(conn transport.PeerConn) {
			defer conn.Close()

			event := handleProbeConnection(ctx, conn, opts.DisplayName)
			select {
			case eventChan <- event:
			case <-ctx.Done():
			}
		}(conn)
	}
}

// handleProbeConnection processes a single probe connection.
func handleProbeConnection(ctx context.Context, conn transport.PeerConn, displayName string) ConnectionEvent {
	event := ConnectionEvent{
		Timestamp:  time.Now(),
		RemoteAddr: remoteAddrString(conn),
	}

	start := time.Now()

	// Set a deadline for the handshake
	handshakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Accept stream from client
	stream, err := conn.AcceptStream(handshakeCtx)
	if err != nil {
		event.Error = fmt.Sprintf("failed to accept stream: %v", err)
		return event
	}
	defer stream.Close()

	// Set deadline on stream
	if err := stream.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		event.Error = fmt.Sprintf("failed to set deadline: %v", err)
		return event
	}

	// Read PEER_HELLO
	reader := protocol.NewFrameReader(stream)
	frame, err := reader.Read()
	if err != nil {
		event.Error = fmt.Sprintf("failed to read frame: %v", err)
		return event
	}

	if frame.Type != protocol.FramePeerHello {
		event.Error = fmt.Sprintf("unexpected frame type: 0x%02x (expected PEER_HELLO)", frame.Type)
		return event
	}

	hello, err := protocol.DecodePeerHello(frame.Payload)
	if err != nil {
		event.Error = fmt.Sprintf("failed to decode PEER_HELLO: %v", err)
		return event
	}

	event.RemoteID = hello.AgentID.String()
	event.RemoteName = hello.DisplayName

	// Generate temporary probe identity and send ACK
	probeID, err := identity.NewAgentID()
	if err != nil {
		event.Error = fmt.Sprintf("failed to generate probe ID: %v", err)
		return event
	}

	ack := &protocol.PeerHello{
		Version:      protocol.ProtocolVersion,
		AgentID:      probeID,
		Timestamp:    uint64(time.Now().UnixNano()),
		Capabilities: []string{},
		DisplayName:  displayName,
	}

	writer := protocol.NewFrameWriter(stream)
	if err := writer.Write(&protocol.Frame{
		Type:     protocol.FramePeerHelloAck,
		StreamID: protocol.ControlStreamID,
		Payload:  ack.Encode(),
	}); err != nil {
		event.Error = fmt.Sprintf("failed to send PEER_HELLO_ACK: %v", err)
		return event
	}

	event.Success = true
	event.RTTMs = float64(time.Since(start).Milliseconds())

	// Close write side to signal we're done and allow client to finish reading
	// before we close the connection entirely
	stream.CloseWrite()

	// Small delay to ensure the client has time to receive the response
	// before the connection is closed (particularly important for QUIC)
	time.Sleep(50 * time.Millisecond)

	return event
}

// remoteAddrString safely extracts the remote address string from a connection.
func remoteAddrString(conn transport.PeerConn) string {
	if addr := conn.RemoteAddr(); addr != nil {
		return addr.String()
	}
	return "unknown"
}

// buildListenerTLSConfig creates a TLS config for the listener.
// If no certificate files are provided, generates ephemeral self-signed certificates.
func buildListenerTLSConfig(opts ListenOptions) (*tls.Config, error) {
	var cert tls.Certificate
	var err error

	if opts.TLSCert != "" && opts.TLSKey != "" {
		// Load provided certificate
		cert, err = tls.LoadX509KeyPair(opts.TLSCert, opts.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate: %w", err)
		}
	} else {
		// Generate ephemeral self-signed certificate
		certPEM, keyPEM, err := transport.GenerateSelfSignedCert("probe-listener", 24*time.Hour)
		if err != nil {
			return nil, fmt.Errorf("failed to generate ephemeral certificate: %w", err)
		}
		cert, err = tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ephemeral certificate: %w", err)
		}
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	// Load CA certificate for client verification (mTLS) if provided
	if opts.TLSCA != "" {
		caCert, err := os.ReadFile(opts.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		config.ClientCAs = caCertPool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return config, nil
}
