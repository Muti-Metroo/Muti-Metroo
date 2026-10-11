package socks5

import (
	"errors"
	"net"
	"time"
)

// ExposeOverSocks backs the CmdExpose (0x07) command. It runs an expose
// session over the raw, already-authenticated SOCKS5 stream, letting a Muti Metroo
// Entry client register ephemeral forward keys over the same endpoint it uses
// for the data plane instead of a separate HTTP/websocket connection. The
// expose Manager satisfies this interface.
type ExposeOverSocks interface {
	// HandleRawConn runs an expose session over conn and blocks until the
	// stream terminates. The connection is closed by the SOCKS5 server after
	// this returns.
	HandleRawConn(conn net.Conn, remoteAddr string)
}

// ErrExposeDisabled is returned when a CmdExpose request arrives but no expose
// handler is configured.
var ErrExposeDisabled = errors.New("expose not enabled")

// handleExpose handles CmdExpose (0x07): after the standard reply it hands the
// raw stream to the expose handler, which runs a full-duplex session over it.
// Authorization is the SOCKS5 handshake performed before dispatch; there is no
// separate gate, matching the route-watch commands.
func (h *Handler) handleExpose(conn net.Conn, req *Request) error {
	if h.expose == nil {
		h.sendReply(conn, ReplyCmdNotSupported, nil, 0)
		return ErrExposeDisabled
	}

	if err := h.sendReply(conn, ReplySucceeded, nil, 0); err != nil {
		return err
	}
	conn.SetDeadline(time.Time{})

	h.expose.HandleRawConn(conn, conn.RemoteAddr().String())
	return nil
}
