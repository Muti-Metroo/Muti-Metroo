package forward

import (
	"context"
	"io"
	"net"
	"time"
)

// tcpBackend is the default Backend that dials a TCP target.
type tcpBackend struct {
	target         string
	connectTimeout time.Duration
}

// NewTCPBackend returns a Backend that dials target on Open.
func NewTCPBackend(target string, connectTimeout time.Duration) Backend {
	return &tcpBackend{target: target, connectTimeout: connectTimeout}
}

func (b *tcpBackend) Target() string {
	return b.target
}

func (b *tcpBackend) Open(ctx context.Context, streamID uint64, key, remoteAddr string) (io.ReadWriteCloser, error) {
	dialer := &net.Dialer{Timeout: b.connectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", b.target)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
