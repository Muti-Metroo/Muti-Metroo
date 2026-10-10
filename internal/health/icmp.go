package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// ICMPProvider provides ICMP session functionality.
type ICMPProvider interface {
	// OpenICMPSession opens an ICMP session to a remote agent.
	// Returns an ICMPSession that can be used to send echo requests.
	OpenICMPSession(ctx context.Context, targetID identity.AgentID, destIP net.IP) (*ICMPSession, error)
}

// ICMPSession represents an active ICMP session with a remote agent.
type ICMPSession struct {
	StreamID uint64
	TargetID identity.AgentID
	DestIP   net.IP

	// Channels for bidirectional communication
	SendEcho    chan *ICMPEchoRequest  // Send echo requests
	ReceiveEcho chan *ICMPEchoResponse // Receive echo replies

	// Done channel closes when session ends
	Done  chan struct{}
	Error error

	// Cleanup function
	Close func()

	mu     sync.Mutex
	closed bool
}

// ICMPEchoRequest represents an echo request to send.
type ICMPEchoRequest struct {
	Identifier uint16
	Sequence   uint16
	Payload    []byte
}

// ICMPEchoResponse represents an echo reply received.
type ICMPEchoResponse struct {
	Identifier uint16
	Sequence   uint16
	Payload    []byte
	Error      string
	SrcIP      net.IP
}

// handleICMPWebSocket handles WebSocket connections for ICMP ping sessions.
// GET /agents/{agent-id}/icmp
func (s *Server) handleICMPWebSocket(w http.ResponseWriter, r *http.Request, targetID identity.AgentID) {
	if s.icmpProvider == nil {
		http.Error(w, "ICMP not available", http.StatusServiceUnavailable)
		return
	}

	// Disable write deadline for long-lived WebSocket connections
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	// Accept WebSocket connection
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{"muti-metroo-icmp"},
	})
	if err != nil {
		http.Error(w, "failed to accept websocket: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// Read initial message with destination IP
	_, initData, err := conn.Read(ctx)
	if err != nil {
		conn.Close(websocket.StatusProtocolError, "failed to read init message")
		return
	}

	var initMsg struct {
		Type   string `json:"type"`
		DestIP string `json:"dest_ip"`
	}
	if err := json.Unmarshal(initData, &initMsg); err != nil {
		conn.Close(websocket.StatusProtocolError, "invalid init message")
		return
	}

	if initMsg.Type != "init" {
		conn.Close(websocket.StatusProtocolError, "expected init message")
		return
	}

	destIP := net.ParseIP(initMsg.DestIP)
	if destIP == nil {
		sendICMPError(conn, ctx, "invalid destination IP")
		conn.Close(websocket.StatusProtocolError, "invalid destination IP")
		return
	}

	// Open ICMP session to target agent
	session, err := s.icmpProvider.OpenICMPSession(ctx, targetID, destIP)
	if err != nil {
		sendICMPError(conn, ctx, err.Error())
		conn.Close(websocket.StatusInternalError, "failed to open ICMP session")
		return
	}
	defer session.Close()

	// Send success response
	successResp := map[string]interface{}{
		"type":    "init_ack",
		"success": true,
	}
	respData, _ := json.Marshal(successResp)
	if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
		return
	}

	// Create context that cancels when either side closes
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// Goroutine: WebSocket -> ICMP Session (echo requests)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		seq := uint16(1)
		for {
			select {
			case <-sessionCtx.Done():
				return
			default:
			}

			_, data, err := conn.Read(sessionCtx)
			if err != nil {
				return
			}

			var echoMsg struct {
				Type     string `json:"type"`
				Sequence int    `json:"sequence"`
				Payload  string `json:"payload"`
			}
			if err := json.Unmarshal(data, &echoMsg); err != nil {
				continue
			}

			if echoMsg.Type != "echo" {
				continue
			}

			// Send echo request
			req := &ICMPEchoRequest{
				Identifier: 1,
				Sequence:   uint16(echoMsg.Sequence),
				Payload:    []byte(echoMsg.Payload),
			}

			select {
			case session.SendEcho <- req:
				seq++
			case <-sessionCtx.Done():
				return
			case <-session.Done:
				return
			}
		}
	}()

	// Goroutine: ICMP Session -> WebSocket (echo replies)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			select {
			case <-sessionCtx.Done():
				return
			case reply, ok := <-session.ReceiveEcho:
				if !ok {
					return
				}

				var respData []byte
				if reply.Error != "" {
					resp := map[string]interface{}{
						"type":     "error",
						"sequence": reply.Sequence,
						"error":    reply.Error,
					}
					respData, _ = json.Marshal(resp)
				} else {
					resp := map[string]interface{}{
						"type":     "reply",
						"sequence": reply.Sequence,
					}
					if len(reply.SrcIP) > 0 {
						resp["src_ip"] = reply.SrcIP.String()
					}
					respData, _ = json.Marshal(resp)
				}

				if err := conn.Write(sessionCtx, websocket.MessageText, respData); err != nil {
					return
				}
			case <-session.Done:
				return
			}
		}
	}()

	// Wait for session to complete
	select {
	case <-session.Done:
	case <-sessionCtx.Done():
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// sendICMPError sends an error response on the WebSocket.
func sendICMPError(conn *websocket.Conn, ctx context.Context, msg string) {
	resp := map[string]interface{}{
		"type":    "init_ack",
		"success": false,
		"error":   msg,
	}
	respData, _ := json.Marshal(resp)
	conn.Write(ctx, websocket.MessageText, respData)
}

// SetICMPProvider sets the ICMP session provider.
func (s *Server) SetICMPProvider(provider ICMPProvider) {
	s.icmpProvider = provider
}
