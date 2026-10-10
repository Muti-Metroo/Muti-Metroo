package expose

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// defaultWriteTimeout bounds a single WriteMessage. Without it a peer that
// stops reading blocks the writer forever: Session.sendRaw holds writeMu for
// the duration, so one stalled peer also stalls the heartbeat loop and any
// stream teardown, and Close() never returns. Generous enough that a slow but
// live peer draining a whole frame is never cut off.
const defaultWriteTimeout = 30 * time.Second

// messageConn is the transport seam beneath a Session. Each call moves exactly
// one whole expose frame's bytes (the 9-byte header plus payload produced by
// EncodeFrame). The implementation is responsible for delimiting frames on the
// wire; Session never sees partial frames.
//
// There is deliberately no context parameter: the only implementation is a raw
// SOCKS5 stream, and teardown is driven by Close() unblocking a blocked read,
// mirroring the route-watch handler.
type messageConn interface {
	ReadMessage() ([]byte, error)
	WriteMessage(b []byte) error
	Close() error
}

// rawConn frames expose messages over a raw stream (the SOCKS5 CmdExpose
// connection) with a u32 big-endian length prefix: [len:u32 BE][bytes]. This
// matches the route-watch wire framing in internal/socks5/routewatch.go.
type rawConn struct {
	conn         net.Conn
	maxRead      int64
	writeTimeout time.Duration
}

func newRawConn(conn net.Conn, maxRead int64) *rawConn {
	return &rawConn{conn: conn, maxRead: maxRead, writeTimeout: defaultWriteTimeout}
}

func (c *rawConn) WriteMessage(b []byte) error {
	// Length prefix and payload go out in one Write so a partial frame cannot
	// interleave with a concurrent writer.
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(b)))
	copy(buf[4:], b)

	if c.writeTimeout > 0 {
		// Best-effort: a conn without deadline support just proceeds unbounded.
		_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
		defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()
	}

	_, err := c.conn.Write(buf)
	return err
}

func (c *rawConn) ReadMessage() ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(c.conn, lenBuf[:]); err != nil {
		return nil, err
	}
	n := int64(binary.BigEndian.Uint32(lenBuf[:]))
	if c.maxRead > 0 && n > c.maxRead {
		return nil, fmt.Errorf("expose: frame too large: %d bytes (max %d)", n, c.maxRead)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (c *rawConn) Close() error {
	return c.conn.Close()
}
