package expose

import (
	"context"
	"io"

	"github.com/Muti-Metroo/Muti-Metroo/internal/forward"
)

// wsBackend adapts a Session into the forward.Backend interface so the
// exit-side handler treats it like any other endpoint.
type wsBackend struct {
	sess  *Session
	label string
}

// NewBackend returns a forward.Backend backed by the given session for the
// given key. The returned backend's Target() string is used as the label
// advertised in the local route table.
func NewBackend(sess *Session, key string) forward.Backend {
	return &wsBackend{sess: sess, label: Label(key)}
}

func (b *wsBackend) Target() string { return b.label }

func (b *wsBackend) Open(ctx context.Context, streamID uint64, key, remoteAddr string) (io.ReadWriteCloser, error) {
	return b.sess.openStream(ctx, key, remoteAddr)
}
