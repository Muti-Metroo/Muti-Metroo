// Package integration provides integration tests for Muti Metroo.
package integration

import (
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// testDNSServer is a tiny UDP DNS responder for tests, backed by
// golang.org/x/net/dns/dnsmessage. It only answers A/IN queries from a fixed
// map; unknown names get an empty answer section (NOERROR with ANCOUNT=0).
//
// Used by both exit_domain_test.go (to prove DNS resolution happens at the
// exit) and udp_relay_test.go (to validate real DNS query traffic through
// the SOCKS5 UDP relay).
type testDNSServer struct {
	pc        net.PacketConn
	records   map[string]net.IP
	queries   atomic.Int64
	lastName  atomic.Value // string
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// newTestDNSServer starts a DNS responder bound to 127.0.0.1 on an ephemeral
// UDP port. The returned server is already serving when this returns.
func newTestDNSServer(t *testing.T, records map[string]net.IP) *testDNSServer {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start test DNS server: %v", err)
	}

	normalized := make(map[string]net.IP, len(records))
	for k, v := range records {
		normalized[strings.ToLower(strings.TrimSuffix(k, "."))] = v
	}

	s := &testDNSServer{pc: pc, records: normalized}
	// Pin the atomic.Value's concrete type to string so subsequent Stores
	// (in serve()) do not panic with a type mismatch.
	s.lastName.Store("")
	s.wg.Add(1)
	go s.serve()
	return s
}

func (s *testDNSServer) Addr() string      { return s.pc.LocalAddr().String() }
func (s *testDNSServer) QueryCount() int64 { return s.queries.Load() }
func (s *testDNSServer) LastName() string {
	v, _ := s.lastName.Load().(string)
	return v
}

// Close stops the server and waits for the serve goroutine to exit.
// Safe to call multiple times.
func (s *testDNSServer) Close() {
	s.closeOnce.Do(func() {
		s.pc.Close()
	})
	s.wg.Wait()
}

func (s *testDNSServer) serve() {
	defer s.wg.Done()

	buf := make([]byte, 512)
	for {
		n, src, err := s.pc.ReadFrom(buf)
		if err != nil {
			// Closed PacketConn returns net.ErrClosed; that is the
			// shutdown path. Anything else is unexpected for a test.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			return
		}
		resp, qname, ok := s.buildResponse(buf[:n])
		if !ok {
			continue
		}
		s.queries.Add(1)
		s.lastName.Store(qname)
		if _, err := s.pc.WriteTo(resp, src); err != nil {
			return
		}
	}
}

// buildResponse parses a single-question DNS query and produces an answer
// from s.records. Returns (response bytes, decoded qname, ok).
func (s *testDNSServer) buildResponse(query []byte) ([]byte, string, bool) {
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil {
		return nil, "", false
	}
	q, err := p.Question()
	if err != nil {
		return nil, "", false
	}

	qname := strings.TrimSuffix(strings.ToLower(q.Name.String()), ".")

	respHdr := dnsmessage.Header{
		ID:                 hdr.ID,
		Response:           true,
		RecursionDesired:   hdr.RecursionDesired,
		RecursionAvailable: true,
	}
	b := dnsmessage.NewBuilder(make([]byte, 0, 256), respHdr)
	if err := b.StartQuestions(); err != nil {
		return nil, "", false
	}
	if err := b.Question(q); err != nil {
		return nil, "", false
	}

	if q.Type == dnsmessage.TypeA && q.Class == dnsmessage.ClassINET {
		if ip, found := s.records[qname]; found {
			if ipv4 := ip.To4(); ipv4 != nil {
				if err := b.StartAnswers(); err != nil {
					return nil, "", false
				}
				rh := dnsmessage.ResourceHeader{
					Name:  q.Name,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   60,
				}
				var a dnsmessage.AResource
				copy(a.A[:], ipv4)
				if err := b.AResource(rh, a); err != nil {
					return nil, "", false
				}
			}
		}
	}

	out, err := b.Finish()
	if err != nil {
		return nil, "", false
	}
	return out, qname, true
}
