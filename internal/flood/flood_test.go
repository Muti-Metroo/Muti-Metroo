package flood

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/crypto"
	"github.com/postalsys/muti-metroo/internal/identity"
	"github.com/postalsys/muti-metroo/internal/protocol"
	"github.com/postalsys/muti-metroo/internal/routing"
)

// ============================================================================
// Mock PeerSender
// ============================================================================

type mockPeerSender struct {
	mu       sync.Mutex
	peers    []identity.AgentID
	messages map[identity.AgentID][]*protocol.Frame
}

func newMockPeerSender() *mockPeerSender {
	return &mockPeerSender{
		messages: make(map[identity.AgentID][]*protocol.Frame),
	}
}

func (m *mockPeerSender) SendToPeer(peerID identity.AgentID, frame *protocol.Frame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[peerID] = append(m.messages[peerID], frame)
	return nil
}

func (m *mockPeerSender) GetPeerIDs() []identity.AgentID {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]identity.AgentID, len(m.peers))
	copy(result, m.peers)
	return result
}

func (m *mockPeerSender) AddPeer(id identity.AgentID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers = append(m.peers, id)
}

func (m *mockPeerSender) GetMessages(peerID identity.AgentID) []*protocol.Frame {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[peerID]
}

func (m *mockPeerSender) TotalMessages() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, msgs := range m.messages {
		count += len(msgs)
	}
	return count
}

// ============================================================================
// Config Tests
// ============================================================================

func TestDefaultFloodConfig(t *testing.T) {
	cfg := DefaultFloodConfig()

	if cfg.SeenCacheTTL != 5*time.Minute {
		t.Errorf("SeenCacheTTL = %v, want 5m", cfg.SeenCacheTTL)
	}
	if cfg.FloodInterval != 1*time.Second {
		t.Errorf("FloodInterval = %v, want 1s", cfg.FloodInterval)
	}
	if cfg.MaxSeenCacheSize != 10000 {
		t.Errorf("MaxSeenCacheSize = %d, want 10000", cfg.MaxSeenCacheSize)
	}
}

// ============================================================================
// Flooder Tests
// ============================================================================

func TestNewFlooder(t *testing.T) {
	localID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	if f == nil {
		t.Fatal("NewFlooder returned nil")
	}
	if f.SeenCacheSize() != 0 {
		t.Errorf("Initial SeenCacheSize = %d, want 0", f.SeenCacheSize())
	}
}

func TestFlooder_HandleRouteAdvertise_NewRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	accepted := f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)
	if !accepted {
		t.Error("First advertisement should be accepted")
	}

	// Should now be seen
	if !f.HasSeen(peerID, 1) {
		t.Error("Advertisement should be in seen cache")
	}

	// Route should be in routing table
	if routeMgr.TotalRoutes() != 1 {
		t.Errorf("TotalRoutes = %d, want 1", routeMgr.TotalRoutes())
	}
}

func TestFlooder_HandleRouteAdvertise_Duplicate(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// First advertisement
	f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)

	// Duplicate
	accepted := f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)
	if accepted {
		t.Error("Duplicate advertisement should be rejected")
	}
}

func TestFlooder_HandleRouteAdvertise_LoopDetection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// Advertisement with our ID in seen-by list (loop)
	seenBy := []identity.AgentID{localID}
	accepted := f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, seenBy)
	if accepted {
		t.Error("Advertisement with our ID in seen-by should be rejected")
	}
}

func TestFlooder_HandleRouteAdvertise_Flood(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	peer3, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)
	sender.AddPeer(peer3)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// Receive from peer1
	f.HandleRouteAdvertise(peer1, peer1, "", 1, routes, nil, nil, nil)

	// Should flood to peer2 and peer3, but not back to peer1
	if len(sender.GetMessages(peer1)) != 0 {
		t.Error("Should not send back to source peer")
	}
	if len(sender.GetMessages(peer2)) != 1 {
		t.Errorf("Should send to peer2, got %d messages", len(sender.GetMessages(peer2)))
	}
	if len(sender.GetMessages(peer3)) != 1 {
		t.Errorf("Should send to peer3, got %d messages", len(sender.GetMessages(peer3)))
	}
}

func TestFlooder_HandleRouteWithdraw(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// First add the route
	f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)

	// Then withdraw
	accepted := f.HandleRouteWithdraw(peerID, peerID, 2, routes, nil)
	if !accepted {
		t.Error("Withdrawal should be accepted")
	}
}

func TestFlooder_HandleRouteWithdraw_Duplicate(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// First withdrawal
	f.HandleRouteWithdraw(peerID, peerID, 1, routes, nil)

	// Duplicate
	accepted := f.HandleRouteWithdraw(peerID, peerID, 1, routes, nil)
	if accepted {
		t.Error("Duplicate withdrawal should be rejected")
	}
}

func TestFlooder_AnnounceLocalRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Add a local route
	routeMgr.AddLocalRoute(routing.MustParseCIDR("10.0.0.0/8"), 10, "")

	// Announce
	f.AnnounceLocalRoutes()

	// Should have sent to peer
	msgs := sender.GetMessages(peerID)
	if len(msgs) != 1 {
		t.Errorf("Should send 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != protocol.FrameRouteAdvertise {
		t.Errorf("Frame type = 0x%02x, want ROUTE_ADVERTISE", msgs[0].Type)
	}
}

func TestFlooder_WithdrawLocalRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Add a local route
	routeMgr.AddLocalRoute(routing.MustParseCIDR("10.0.0.0/8"), 10, "")

	// Withdraw
	f.WithdrawLocalRoutes()

	msgs := sender.GetMessages(peerID)
	if len(msgs) != 1 {
		t.Errorf("Should send 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != protocol.FrameRouteWithdraw {
		t.Errorf("Frame type = 0x%02x, want ROUTE_WITHDRAW", msgs[0].Type)
	}
}

func TestFlooder_WithdrawLocalForwardRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routeMgr.AddLocalForwardRoute("ssh-prod", "10.0.0.5:22", 0)

	f.WithdrawLocalForwardRoute("ssh-prod", "10.0.0.5:22")

	msgs := sender.GetMessages(peerID)
	if len(msgs) != 1 {
		t.Fatalf("Should send 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != protocol.FrameRouteWithdraw {
		t.Fatalf("Frame type = 0x%02x, want ROUTE_WITHDRAW", msgs[0].Type)
	}
	withdraw, err := protocol.DecodeRouteWithdraw(msgs[0].Payload)
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw: %v", err)
	}
	if withdraw.OriginAgent != localID {
		t.Errorf("OriginAgent = %v, want %v", withdraw.OriginAgent, localID)
	}
	if withdraw.Sequence == 0 {
		t.Errorf("Sequence should be > 0")
	}
	if len(withdraw.Routes) != 1 {
		t.Fatalf("Routes len = %d, want 1", len(withdraw.Routes))
	}
	if withdraw.Routes[0].AddressFamily != protocol.AddrFamilyForward {
		t.Errorf("AddressFamily = 0x%02x, want AddrFamilyForward", withdraw.Routes[0].AddressFamily)
	}
	if k := protocol.DecodeForwardKey(withdraw.Routes[0].Prefix); k != "ssh-prod" {
		t.Errorf("DecodeForwardKey = %q, want %q", k, "ssh-prod")
	}
}

func TestFlooder_WithdrawLocalDomainRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	f.WithdrawLocalDomainRoute("*.example.com", true)

	msgs := sender.GetMessages(peerID)
	if len(msgs) != 1 {
		t.Fatalf("Should send 1 message, got %d", len(msgs))
	}
	withdraw, err := protocol.DecodeRouteWithdraw(msgs[0].Payload)
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw: %v", err)
	}
	if withdraw.Routes[0].AddressFamily != protocol.AddrFamilyDomain {
		t.Errorf("AddressFamily = 0x%02x, want AddrFamilyDomain", withdraw.Routes[0].AddressFamily)
	}
	if withdraw.Routes[0].PrefixLength != 1 {
		t.Errorf("PrefixLength = %d, want 1 (wildcard)", withdraw.Routes[0].PrefixLength)
	}
	if p := protocol.DecodeDomainPrefix(withdraw.Routes[0].Prefix); p != "*.example.com" {
		t.Errorf("DecodeDomainPrefix = %q, want %q", p, "*.example.com")
	}
}

func TestFlooder_WithdrawLocalCIDRRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	f.WithdrawLocalCIDRRoute(routing.MustParseCIDR("10.99.0.0/24"), 5)

	msgs := sender.GetMessages(peerID)
	if len(msgs) != 1 {
		t.Fatalf("Should send 1 message, got %d", len(msgs))
	}
	withdraw, err := protocol.DecodeRouteWithdraw(msgs[0].Payload)
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw: %v", err)
	}
	if withdraw.Routes[0].AddressFamily != protocol.AddrFamilyIPv4 {
		t.Errorf("AddressFamily = 0x%02x, want AddrFamilyIPv4", withdraw.Routes[0].AddressFamily)
	}
	if withdraw.Routes[0].Metric != 5 {
		t.Errorf("Metric = %d, want 5", withdraw.Routes[0].Metric)
	}
}

func TestFlooder_HandleRouteWithdraw_Forward(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Pre-populate the forward table with a route from origin via peer.
	routeMgr.ProcessForwardRouteAdvertise(
		peerID,
		originID,
		1,
		[]routing.ForwardRouteEntry{{Key: "k", Target: "127.0.0.1:9000"}},
		nil,
		nil,
	)
	if !routeMgr.ForwardTable().HasRoute("k", originID) {
		t.Fatalf("setup: forward route should exist before withdraw")
	}

	withdrawRoute := protocol.Route{
		AddressFamily: protocol.AddrFamilyForward,
		Prefix:        protocol.EncodeForwardKeyWithTarget("k", "127.0.0.1:9000"),
	}
	if !f.HandleRouteWithdraw(peerID, originID, 2, []protocol.Route{withdrawRoute}, nil) {
		t.Fatal("HandleRouteWithdraw should accept the frame")
	}
	if routeMgr.ForwardTable().HasRoute("k", originID) {
		t.Errorf("forward route still present after withdraw")
	}
}

func TestFlooder_HandleRouteWithdraw_Domain(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routeMgr.ProcessDomainRouteAdvertise(
		peerID,
		originID,
		1,
		[]routing.DomainRouteEntry{{Pattern: "*.example.com", IsWildcard: true}},
		nil,
		nil,
	)
	if !routeMgr.DomainTable().HasRoute("*.example.com", originID) {
		t.Fatalf("setup: domain route should exist before withdraw")
	}

	withdrawRoute := protocol.Route{
		AddressFamily: protocol.AddrFamilyDomain,
		PrefixLength:  1,
		Prefix:        protocol.EncodeDomainPrefix("*.example.com"),
	}
	if !f.HandleRouteWithdraw(peerID, originID, 2, []protocol.Route{withdrawRoute}, nil) {
		t.Fatal("HandleRouteWithdraw should accept the frame")
	}
	if routeMgr.DomainTable().HasRoute("*.example.com", originID) {
		t.Errorf("domain route still present after withdraw")
	}
}

func TestFlooder_SendFullTable(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Add a local route
	routeMgr.AddLocalRoute(routing.MustParseCIDR("10.0.0.0/8"), 10, "")

	// Send full table to peer1 only
	f.SendFullTable(peer1)

	// Only peer1 should receive (CIDR route + local presence)
	if len(sender.GetMessages(peer1)) != 2 {
		t.Errorf("peer1 should receive 2 messages (CIDR + local presence), got %d", len(sender.GetMessages(peer1)))
	}
	if len(sender.GetMessages(peer2)) != 0 {
		t.Error("peer2 should not receive full table")
	}
}

func TestFlooder_ClearSeenCache(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// Add some entries
	f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)
	f.HandleRouteAdvertise(peerID, peerID, "", 2, routes, nil, nil, nil)

	if f.SeenCacheSize() != 2 {
		t.Errorf("SeenCacheSize = %d, want 2", f.SeenCacheSize())
	}

	// Clear
	f.ClearSeenCache()

	if f.SeenCacheSize() != 0 {
		t.Errorf("SeenCacheSize after clear = %d, want 0", f.SeenCacheSize())
	}
}

func TestFlooder_HasSeen(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	if f.HasSeen(peerID, 1) {
		t.Error("Should not have seen yet")
	}

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)

	if !f.HasSeen(peerID, 1) {
		t.Error("Should have seen after handling")
	}
}

func TestFlooder_IPv6Routes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv6,
			PrefixLength:  32,
			Prefix:        []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			Metric:        10,
		},
	}

	accepted := f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, nil, nil, nil)
	if !accepted {
		t.Error("IPv6 route should be accepted")
	}

	if routeMgr.TotalRoutes() != 1 {
		t.Errorf("TotalRoutes = %d, want 1", routeMgr.TotalRoutes())
	}
}

func TestFlooder_Stop(t *testing.T) {
	localID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()
	cfg.SeenCacheTTL = 100 * time.Millisecond

	f := NewFlooder(cfg, localID, routeMgr, sender)

	// Stop multiple times should not panic
	f.Stop()
	f.Stop()
}

// ============================================================================
// Advertisement Key Tests
// ============================================================================

func TestAdvertisementKey_Uniqueness(t *testing.T) {
	id1, _ := identity.NewAgentID()
	id2, _ := identity.NewAgentID()

	key1 := AdvertisementKey{OriginAgent: id1, Sequence: 1}
	key2 := AdvertisementKey{OriginAgent: id1, Sequence: 2}
	key3 := AdvertisementKey{OriginAgent: id2, Sequence: 1}

	if key1 == key2 {
		t.Error("Different sequences should be different keys")
	}
	if key1 == key3 {
		t.Error("Different origins should be different keys")
	}
}

// ============================================================================
// Sleep/Wake Command Signature Verification Tests
// ============================================================================

func TestVerifySleepCommand_NoKeyConfigured_AcceptsAll(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// No signing key configured
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Unsigned command should be accepted
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{}, // Zero signature (unsigned)
		SeenBy:      []identity.AgentID{},
	}

	accepted := f.HandleSleepCommand(peerID, cmd)
	if !accepted {
		t.Error("Unsigned command should be accepted when no signing key configured")
	}
}

func TestVerifySleepCommand_ValidSignature_Accepts(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Create and sign command
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	accepted := f.HandleSleepCommand(peerID, cmd)
	if !accepted {
		t.Error("Validly signed command should be accepted")
	}
}

func TestVerifySleepCommand_InvalidSignature_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Create command with garbage signature
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{0xFF, 0xDE, 0xAD, 0xBE, 0xEF}, // Invalid signature
		SeenBy:      []identity.AgentID{},
	}

	accepted := f.HandleSleepCommand(peerID, cmd)
	if accepted {
		t.Error("Invalid signature should be rejected")
	}
}

func TestVerifySleepCommand_MissingSignature_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Unsigned command (zero signature) when key is configured
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{}, // Zero = unsigned
		SeenBy:      []identity.AgentID{},
	}

	accepted := f.HandleSleepCommand(peerID, cmd)
	if accepted {
		t.Error("Unsigned command should be rejected when signing key is configured")
	}
}

func TestVerifySleepCommand_ExpiredTimestamp_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey
	cfg.TimestampWindow = 5 * time.Minute

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Command with old timestamp (10 minutes ago)
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Add(-10 * time.Minute).Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	accepted := f.HandleSleepCommand(peerID, cmd)
	if accepted {
		t.Error("Expired timestamp should be rejected")
	}
}

func TestVerifySleepCommand_FutureTimestamp_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey
	cfg.TimestampWindow = 5 * time.Minute

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Command with future timestamp (10 minutes ahead)
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Add(10 * time.Minute).Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	accepted := f.HandleSleepCommand(peerID, cmd)
	if accepted {
		t.Error("Future timestamp should be rejected")
	}
}

func TestVerifyWakeCommand_NoKeyConfigured_AcceptsAll(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// No signing key configured
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Unsigned command should be accepted
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{}, // Zero signature (unsigned)
		SeenBy:      []identity.AgentID{},
	}

	accepted := f.HandleWakeCommand(peerID, cmd)
	if !accepted {
		t.Error("Unsigned wake command should be accepted when no signing key configured")
	}
}

func TestVerifyWakeCommand_ValidSignature_Accepts(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Create and sign command
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	accepted := f.HandleWakeCommand(peerID, cmd)
	if !accepted {
		t.Error("Validly signed wake command should be accepted")
	}
}

func TestVerifyWakeCommand_InvalidSignature_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Create command with garbage signature
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{0xCA, 0xFE, 0xBA, 0xBE}, // Invalid signature
		SeenBy:      []identity.AgentID{},
	}

	accepted := f.HandleWakeCommand(peerID, cmd)
	if accepted {
		t.Error("Invalid signature should be rejected")
	}
}

func TestVerifyWakeCommand_MissingSignature_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Unsigned command (zero signature) when key is configured
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{}, // Zero = unsigned
		SeenBy:      []identity.AgentID{},
	}

	accepted := f.HandleWakeCommand(peerID, cmd)
	if accepted {
		t.Error("Unsigned wake command should be rejected when signing key is configured")
	}
}

func TestVerifyWakeCommand_ExpiredTimestamp_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey
	cfg.TimestampWindow = 5 * time.Minute

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Command with old timestamp (10 minutes ago)
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Add(-10 * time.Minute).Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	accepted := f.HandleWakeCommand(peerID, cmd)
	if accepted {
		t.Error("Wake command with expired timestamp should be rejected")
	}
}

func TestVerifyWakeCommand_FutureTimestamp_Rejects(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey
	cfg.TimestampWindow = 5 * time.Minute

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Command with future timestamp (10 minutes ahead)
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Add(10 * time.Minute).Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	accepted := f.HandleWakeCommand(peerID, cmd)
	if accepted {
		t.Error("Wake command with future timestamp should be rejected")
	}
}

func TestHandleSleepCommand_SignedCommand_Floods(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Create signed command
	cmd := &protocol.SleepCommand{
		OriginAgent: peer1,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	// Handle from peer1
	accepted := f.HandleSleepCommand(peer1, cmd)
	if !accepted {
		t.Error("Signed command should be accepted")
	}

	// Should flood to peer2 (not back to peer1)
	msgs := sender.GetMessages(peer2)
	if len(msgs) != 1 {
		t.Errorf("Should flood to peer2, got %d messages", len(msgs))
	}
	if len(sender.GetMessages(peer1)) != 0 {
		t.Error("Should not flood back to source peer")
	}
}

func TestHandleSleepCommand_UnsignedCommand_NoKey_Floods(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)

	// No signing key configured (backward compatible mode)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Unsigned command
	cmd := &protocol.SleepCommand{
		OriginAgent: peer1,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{}, // Zero = unsigned
		SeenBy:      []identity.AgentID{},
	}

	// Handle from peer1
	accepted := f.HandleSleepCommand(peer1, cmd)
	if !accepted {
		t.Error("Unsigned command should be accepted when no key configured")
	}

	// Should flood to peer2
	if len(sender.GetMessages(peer2)) != 1 {
		t.Error("Should flood unsigned command when no key configured")
	}
}

func TestHandleSleepCommand_UnsignedCommand_WithKey_Rejected(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Unsigned command
	cmd := &protocol.SleepCommand{
		OriginAgent: peer1,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{}, // Zero = unsigned
		SeenBy:      []identity.AgentID{},
	}

	// Handle from peer1
	accepted := f.HandleSleepCommand(peer1, cmd)
	if accepted {
		t.Error("Unsigned command should be rejected when key is configured")
	}

	// Should NOT flood
	if sender.TotalMessages() != 0 {
		t.Error("Should not flood rejected command")
	}
}

func TestHandleWakeCommand_SignedCommand_Floods(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)

	// Generate signing keypair
	kp, err := crypto.GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("GenerateSigningKeypair() error = %v", err)
	}

	cfg := DefaultFloodConfig()
	cfg.SigningPublicKey = &kp.PublicKey

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Create signed command
	cmd := &protocol.WakeCommand{
		OriginAgent: peer1,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		SeenBy:      []identity.AgentID{},
	}
	cmd.Signature = crypto.Sign(kp.PrivateKey, cmd.SignableBytes())

	// Handle from peer1
	accepted := f.HandleWakeCommand(peer1, cmd)
	if !accepted {
		t.Error("Signed wake command should be accepted")
	}

	// Should flood to peer2 (not back to peer1)
	msgs := sender.GetMessages(peer2)
	if len(msgs) != 1 {
		t.Errorf("Should flood wake command to peer2, got %d messages", len(msgs))
	}
}

func TestHandleSleepCommand_Deduplication(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   12345,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{},
		SeenBy:      []identity.AgentID{},
	}

	// First time should accept
	if !f.HandleSleepCommand(peerID, cmd) {
		t.Error("First sleep command should be accepted")
	}

	// Second time should reject (duplicate)
	if f.HandleSleepCommand(peerID, cmd) {
		t.Error("Duplicate sleep command should be rejected")
	}
}

func TestHandleWakeCommand_Deduplication(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   54321,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{},
		SeenBy:      []identity.AgentID{},
	}

	// First time should accept
	if !f.HandleWakeCommand(peerID, cmd) {
		t.Error("First wake command should be accepted")
	}

	// Second time should reject (duplicate)
	if f.HandleWakeCommand(peerID, cmd) {
		t.Error("Duplicate wake command should be rejected")
	}
}

func TestHandleSleepCommand_LoopDetection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Command with our ID in SeenBy (loop)
	cmd := &protocol.SleepCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{},
		SeenBy:      []identity.AgentID{localID}, // Loop!
	}

	accepted := f.HandleSleepCommand(peerID, cmd)
	if accepted {
		t.Error("Sleep command with our ID in SeenBy should be rejected (loop)")
	}
}

func TestHandleWakeCommand_LoopDetection(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Command with our ID in SeenBy (loop)
	cmd := &protocol.WakeCommand{
		OriginAgent: peerID,
		CommandID:   1,
		Timestamp:   uint64(time.Now().Unix()),
		Signature:   [64]byte{},
		SeenBy:      []identity.AgentID{localID}, // Loop!
	}

	accepted := f.HandleWakeCommand(peerID, cmd)
	if accepted {
		t.Error("Wake command with our ID in SeenBy should be rejected (loop)")
	}
}

func TestSleepCommandSeenCacheSize(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Initially empty
	if f.SleepCommandSeenCacheSize() != 0 {
		t.Errorf("Initial SleepCommandSeenCacheSize = %d, want 0", f.SleepCommandSeenCacheSize())
	}

	// Add some commands
	for i := uint64(1); i <= 5; i++ {
		cmd := &protocol.SleepCommand{
			OriginAgent: peerID,
			CommandID:   i,
			Timestamp:   uint64(time.Now().Unix()),
			Signature:   [64]byte{},
			SeenBy:      []identity.AgentID{},
		}
		f.HandleSleepCommand(peerID, cmd)
	}

	if f.SleepCommandSeenCacheSize() != 5 {
		t.Errorf("SleepCommandSeenCacheSize = %d, want 5", f.SleepCommandSeenCacheSize())
	}
}

func TestFlooder_AnnounceLocalRoutes_AlwaysIncludesAgentPresence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// No exit routes configured - AnnounceLocalRoutes should still send
	f.AnnounceLocalRoutes()

	msgs := sender.GetMessages(peer1)
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message to peer1, got %d", len(msgs))
	}

	// Decode the advertisement
	adv, err := protocol.DecodeRouteAdvertise(msgs[0].Payload)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise() error = %v", err)
	}

	// Should have exactly 1 route: the agent presence route
	if len(adv.Routes) != 1 {
		t.Fatalf("Expected 1 route, got %d", len(adv.Routes))
	}

	if adv.Routes[0].AddressFamily != protocol.AddrFamilyAgent {
		t.Errorf("Route AddressFamily = %d, want %d (AddrFamilyAgent)", adv.Routes[0].AddressFamily, protocol.AddrFamilyAgent)
	}

	decodedID := protocol.DecodeAgentPrefix(adv.Routes[0].Prefix)
	if decodedID != localID {
		t.Errorf("Agent presence route ID = %s, want %s", decodedID.String(), localID.String())
	}
}

func TestFlooder_HandleRouteAdvertise_AgentPresenceRoute(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	remoteAgent, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Simulate receiving a route advertisement with agent presence route
	path := protocol.EncodePath([]identity.AgentID{peer1, remoteAgent})
	encPath := &protocol.EncryptedData{
		Encrypted: false,
		Data:      path,
	}

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyAgent,
			PrefixLength:  0,
			Prefix:        protocol.EncodeAgentPrefix(remoteAgent),
			Metric:        0,
		},
	}

	handled := f.HandleRouteAdvertise(peer1, remoteAgent, "", 1, routes, nil, encPath, []identity.AgentID{remoteAgent})
	if !handled {
		t.Error("HandleRouteAdvertise should return true for new advertisement")
	}

	// Verify agent is now in the agent table
	agentRoute := routeMgr.LookupAgent(remoteAgent)
	if agentRoute == nil {
		t.Fatal("LookupAgent should find the remote agent after advertisement")
	}
	if agentRoute.NextHop != peer1 {
		t.Errorf("NextHop = %s, want %s", agentRoute.NextHop.ShortString(), peer1.ShortString())
	}
}

func TestFlooder_SendFullTable_IncludesAgentPresenceRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()
	peer2, _ := identity.NewAgentID()
	remoteAgent, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)
	sender.AddPeer(peer2)

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Add an agent route learned from peer1
	routeMgr.ProcessAgentRouteAdvertise(
		peer1, remoteAgent, 1, remoteAgent,
		[]identity.AgentID{peer1, remoteAgent}, nil, 1,
	)

	// Send full table to peer2
	f.SendFullTable(peer2)

	msgs := sender.GetMessages(peer2)
	if len(msgs) == 0 {
		t.Fatal("Expected at least 1 message to peer2")
	}

	// Find the agent presence route in sent messages
	found := false
	for _, msg := range msgs {
		adv, err := protocol.DecodeRouteAdvertise(msg.Payload)
		if err != nil {
			continue
		}
		for _, r := range adv.Routes {
			if r.AddressFamily == protocol.AddrFamilyAgent {
				decodedID := protocol.DecodeAgentPrefix(r.Prefix)
				if decodedID == remoteAgent {
					found = true
				}
			}
		}
	}

	if !found {
		t.Error("SendFullTable should include agent presence route for remote agent")
	}
}

func TestFlooder_SendFullTable_IncludesLocalAgentPresence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// No routes added - agent has no exit routes (transit-only)
	f.SendFullTable(peer1)

	msgs := sender.GetMessages(peer1)
	if len(msgs) == 0 {
		t.Fatal("Expected at least 1 message to peer1 (local presence)")
	}

	// Find the local agent's own presence route
	found := false
	for _, msg := range msgs {
		adv, err := protocol.DecodeRouteAdvertise(msg.Payload)
		if err != nil {
			continue
		}
		if adv.OriginAgent != localID {
			continue
		}
		for _, r := range adv.Routes {
			if r.AddressFamily == protocol.AddrFamilyAgent {
				decodedID := protocol.DecodeAgentPrefix(r.Prefix)
				if decodedID == localID {
					found = true
					if r.Metric != 0 {
						t.Errorf("Local agent presence metric = %d, want 0", r.Metric)
					}
				}
			}
		}
	}

	if !found {
		t.Error("SendFullTable should include local agent's own presence route")
	}
}

func TestFlooder_SequenceStartsAboveZero(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peer1, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peer1)

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	// Announce local routes and check the sequence in the sent frame
	f.AnnounceLocalRoutes()

	msgs := sender.GetMessages(peer1)
	if len(msgs) == 0 {
		t.Fatal("Expected message from AnnounceLocalRoutes")
	}

	adv, err := protocol.DecodeRouteAdvertise(msgs[0].Payload)
	if err != nil {
		t.Fatalf("Failed to decode route advertise: %v", err)
	}

	// Route sequence comes from routing.Manager which starts from time.Now().UnixMilli()
	if adv.Sequence < 1_000_000_000 {
		t.Errorf("Route sequence = %d, expected time-based value above 1 billion", adv.Sequence)
	}

	// Node info sequence starts from time.Now().UnixMilli() too
	if f.nodeInfoSeq < 1_000_000_000 {
		t.Errorf("nodeInfoSeq = %d, expected time-based value above 1 billion", f.nodeInfoSeq)
	}
}

func TestFlooder_ClearSeenCacheForOrigin(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyIPv4,
			PrefixLength:  8,
			Prefix:        []byte{10, 0, 0, 0},
			Metric:        10,
		},
	}

	// Add entries from two different origins
	f.HandleRouteAdvertise(peerA, peerA, "", 1, routes, nil, nil, nil)
	f.HandleRouteAdvertise(peerA, peerA, "", 2, routes, nil, nil, nil)
	f.HandleRouteAdvertise(peerB, peerB, "", 1, routes, nil, nil, nil)

	if f.SeenCacheSize() != 3 {
		t.Fatalf("SeenCacheSize = %d, want 3", f.SeenCacheSize())
	}

	// Clear entries for peerA only
	f.ClearSeenCacheForOrigin(peerA)

	if f.SeenCacheSize() != 1 {
		t.Errorf("SeenCacheSize after clearing peerA = %d, want 1", f.SeenCacheSize())
	}

	// peerB's entry should still be there
	if !f.HasSeen(peerB, 1) {
		t.Error("peerB entry should still be in seen cache")
	}

	// peerA's entries should be gone
	if f.HasSeen(peerA, 1) {
		t.Error("peerA seq=1 should be cleared")
	}
	if f.HasSeen(peerA, 2) {
		t.Error("peerA seq=2 should be cleared")
	}
}

func TestFlooder_ReconnectAcceptsAdvertisements(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerA, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerA)

	cfg := DefaultFloodConfig()
	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{
			AddressFamily: protocol.AddrFamilyAgent,
			PrefixLength:  0,
			Prefix:        protocol.EncodeAgentPrefix(peerA),
			Metric:        0,
		},
	}

	// First advertisement - should be accepted
	accepted := f.HandleRouteAdvertise(peerA, peerA, "", 1, routes, nil, nil, nil)
	if !accepted {
		t.Fatal("First advertisement should be accepted")
	}

	// Same advertisement again - should be rejected (seen cache)
	accepted = f.HandleRouteAdvertise(peerA, peerA, "", 1, routes, nil, nil, nil)
	if accepted {
		t.Fatal("Duplicate advertisement should be rejected")
	}

	// Simulate peer disconnect: clear seen cache for this origin
	f.ClearSeenCacheForOrigin(peerA)

	// Same advertisement after cache clear - should be accepted again
	accepted = f.HandleRouteAdvertise(peerA, peerA, "", 1, routes, nil, nil, nil)
	if !accepted {
		t.Fatal("Advertisement after cache clear should be accepted")
	}
}

// TestFlooder_HandleRouteAdvertise_PropagatesComment is the receive-side
// half of the propagation contract: a route advertisement carrying a
// non-empty Comments slice deposits the comment on the peer-learned
// Route in the routing table, where the dashboard can read it back.
func TestFlooder_HandleRouteAdvertise_PropagatesComment(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{
		{AddressFamily: protocol.AddrFamilyIPv4, PrefixLength: 8, Prefix: []byte{10, 0, 0, 0}, Metric: 10},
	}
	comments := []string{"office LAN"}
	if !f.HandleRouteAdvertise(peerID, peerID, "", 1, routes, comments, nil, nil) {
		t.Fatal("advertisement should be accepted")
	}

	stored := routeMgr.Table().GetRoute(routing.MustParseCIDR("10.0.0.0/8"))
	if stored == nil {
		t.Fatal("route not stored")
	}
	if stored.Comment != "office LAN" {
		t.Errorf("stored Comment = %q, want %q", stored.Comment, "office LAN")
	}
}

// TestFlooder_AnnounceLocalRoutes_EmitsComments confirms the local
// originator pulls Comment off LocalRoute and includes it in the
// outgoing advertisement parallel to Routes.
func TestFlooder_AnnounceLocalRoutes_EmitsComments(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	cfg := DefaultFloodConfig()

	f := NewFlooder(cfg, localID, routeMgr, sender)
	defer f.Stop()

	if !routeMgr.AddLocalRoute(routing.MustParseCIDR("10.0.0.0/8"), 0, "office LAN") {
		t.Fatal("AddLocalRoute should succeed")
	}

	f.AnnounceLocalRoutes()

	msgs := sender.GetMessages(peerID)
	if len(msgs) == 0 {
		t.Fatal("no frame sent to peer")
	}
	frame := msgs[len(msgs)-1]
	adv, err := protocol.DecodeRouteAdvertise(frame.Payload)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise: %v", err)
	}
	// Find the IPv4 10.0.0.0/8 route and check its comment slot.
	idx := -1
	for i, r := range adv.Routes {
		if r.AddressFamily == protocol.AddrFamilyIPv4 && r.PrefixLength == 8 {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("local CIDR route missing from advertisement")
	}
	if idx >= len(adv.Comments) {
		t.Fatalf("Comments slice missing entry at index %d (len=%d)", idx, len(adv.Comments))
	}
	if adv.Comments[idx] != "office LAN" {
		t.Errorf("Comments[%d] = %q, want %q", idx, adv.Comments[idx], "office LAN")
	}
}

// TestFlooder_LocalCommentEndToEnd exercises the full local-side path:
// the Flooder builds an advertisement from a LocalRoute carrying a
// comment, the bytes go through the wire encoder, and the receive-side
// HandleRouteAdvertise on a fresh routing.Manager stores the comment.
// This proves the comment survives every layer between two agents.
func TestFlooder_LocalCommentEndToEnd(t *testing.T) {
	originID, _ := identity.NewAgentID()
	receiverID, _ := identity.NewAgentID()

	// Sender side: build a local advertisement.
	senderRouteMgr := routing.NewManager(originID)
	senderSender := newMockPeerSender()
	senderSender.AddPeer(receiverID)
	sender := NewFlooder(DefaultFloodConfig(), originID, senderRouteMgr, senderSender)
	defer sender.Stop()
	if !senderRouteMgr.AddLocalRoute(routing.MustParseCIDR("10.0.0.0/8"), 0, "office LAN") {
		t.Fatal("AddLocalRoute should succeed")
	}
	sender.AnnounceLocalRoutes()
	msgs := senderSender.GetMessages(receiverID)
	if len(msgs) == 0 {
		t.Fatal("no advertisement produced")
	}
	advBytes := msgs[len(msgs)-1].Payload

	// Wire round-trip via the actual codec.
	adv, err := protocol.DecodeRouteAdvertise(advBytes)
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise: %v", err)
	}

	// Receiver side: hand the decoded message to the receiver flooder
	// just like Agent.handleRouteAdvertise does.
	recvRouteMgr := routing.NewManager(receiverID)
	recvSender := newMockPeerSender()
	receiver := NewFlooder(DefaultFloodConfig(), receiverID, recvRouteMgr, recvSender)
	defer receiver.Stop()

	if !receiver.HandleRouteAdvertise(originID, adv.OriginAgent, adv.OriginDisplayName, adv.Sequence, adv.Routes, adv.Comments, adv.EncPath, adv.SeenBy) {
		t.Fatal("receiver should accept the advertisement")
	}

	stored := recvRouteMgr.Table().GetRoute(routing.MustParseCIDR("10.0.0.0/8"))
	if stored == nil {
		t.Fatal("receiver did not store the route")
	}
	if stored.Comment != "office LAN" {
		t.Errorf("stored Comment = %q, want %q", stored.Comment, "office LAN")
	}
}

// Re-advertising a route learned from another agent must carry that agent's
// own sequence. Stamping a locally minted one makes the copy look strictly
// newer than anything the origin sends, so peers reject the origin's genuine
// advertisements, stop refreshing LastUpdate, and drop the route at RouteTTL.
func TestFlooder_SendFullTable_PreservesOriginSequence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	learnedFrom, _ := identity.NewAgentID()
	newPeer, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	const originSeq = 42
	routes := []protocol.Route{{
		AddressFamily: protocol.AddrFamilyIPv4,
		PrefixLength:  8,
		Prefix:        []byte{10, 0, 0, 0},
		Metric:        10,
	}}
	if !f.HandleRouteAdvertise(learnedFrom, originID, "", originSeq, routes, nil, nil, nil) {
		t.Fatal("advertisement from origin should be accepted")
	}

	sender.AddPeer(newPeer)
	f.SendFullTable(newPeer)

	var sawOrigin bool
	for _, frame := range sender.GetMessages(newPeer) {
		if frame.Type != protocol.FrameRouteAdvertise {
			continue
		}
		adv, err := protocol.DecodeRouteAdvertise(frame.Payload)
		if err != nil {
			t.Fatalf("DecodeRouteAdvertise: %v", err)
		}
		if adv.OriginAgent != originID {
			continue
		}
		sawOrigin = true
		if adv.Sequence != originSeq {
			t.Errorf("re-advertised sequence = %d, want %d (the origin's own sequence)", adv.Sequence, originSeq)
		}
	}
	if !sawOrigin {
		t.Fatal("full table did not include the learned route's origin")
	}
}

// The local agent is the authority on its own routes, so its advertisements
// still carry a freshly minted, increasing sequence.
func TestFlooder_SendFullTable_LocalOriginUsesFreshSequence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerA, _ := identity.NewAgentID()
	peerB, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	_, network, _ := net.ParseCIDR("192.168.0.0/16")
	routeMgr.AddLocalRoute(network, 5, "")

	sender := newMockPeerSender()
	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	seqFor := func(peer identity.AgentID) uint64 {
		var seq uint64
		for _, frame := range sender.GetMessages(peer) {
			if frame.Type != protocol.FrameRouteAdvertise {
				continue
			}
			adv, err := protocol.DecodeRouteAdvertise(frame.Payload)
			if err != nil {
				t.Fatalf("DecodeRouteAdvertise: %v", err)
			}
			if adv.OriginAgent == localID && adv.Sequence > seq {
				seq = adv.Sequence
			}
		}
		return seq
	}

	sender.AddPeer(peerA)
	f.SendFullTable(peerA)
	first := seqFor(peerA)
	if first == 0 {
		t.Fatal("no local-origin advertisement sent")
	}

	sender.AddPeer(peerB)
	f.SendFullTable(peerB)
	if second := seqFor(peerB); second <= first {
		t.Errorf("local sequence did not advance: first=%d second=%d", first, second)
	}
}

// The wire format carries the route count in a single byte, so an origin with
// more than 255 routes must be split across advertisements. Without the split
// the frame is undecodable and the peer drops the connection.
//
// Each chunk must also carry its OWN sequence: receivers dedupe on
// (OriginAgent, Sequence), so chunks sharing one sequence are discarded as
// duplicates and only the first would ever propagate.
func TestFlooder_AnnounceLocalRoutes_SplitsLargeRouteSets(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	const routeCount = 600
	for i := 0; i < routeCount; i++ {
		_, network, err := net.ParseCIDR(fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
		if err != nil {
			t.Fatalf("ParseCIDR: %v", err)
		}
		routeMgr.AddLocalRoute(network, 1, "")
	}

	sender := newMockPeerSender()
	sender.AddPeer(peerID)
	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	f.AnnounceLocalRoutes()

	frames := sender.GetMessages(peerID)
	if len(frames) < 2 {
		t.Fatalf("expected the route set to be split across frames, got %d", len(frames))
	}

	total := 0
	seenSequences := make(map[uint64]bool)
	for i, frame := range frames {
		adv, err := protocol.DecodeRouteAdvertise(frame.Payload)
		if err != nil {
			t.Fatalf("frame %d: DecodeRouteAdvertise: %v", i, err)
		}
		if len(adv.Routes) > protocol.MaxRoutesPerMessage {
			t.Errorf("frame %d carries %d routes, want <= %d", i, len(adv.Routes), protocol.MaxRoutesPerMessage)
		}
		if seenSequences[adv.Sequence] {
			t.Errorf("frame %d reuses sequence %d; a receiver would drop it as a duplicate",
				i, adv.Sequence)
		}
		seenSequences[adv.Sequence] = true
		total += len(adv.Routes)
	}
	// routeCount CIDR routes plus the agent presence route.
	if total != routeCount+1 {
		t.Errorf("routes across all frames = %d, want %d", total, routeCount+1)
	}
}

// End-to-end check that a split route set actually lands in a receiving
// agent's table. Splitting the frames is only half the fix: if every chunk
// carries the same sequence, the receiver's (origin, sequence) seen-cache
// discards all but the first and most of the table silently never arrives.
func TestFlooder_LargeRouteSetReachesReceiver(t *testing.T) {
	originID, _ := identity.NewAgentID()
	receiverID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()

	// Origin side: publish more routes than one advertisement can carry.
	originMgr := routing.NewManager(originID)
	const routeCount = 600
	for i := 0; i < routeCount; i++ {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
		originMgr.AddLocalRoute(network, 1, "")
	}
	originSender := newMockPeerSender()
	originSender.AddPeer(peerID)
	origin := NewFlooder(DefaultFloodConfig(), originID, originMgr, originSender)
	defer origin.Stop()
	origin.AnnounceLocalRoutes()

	// Receiver side: feed it every frame the origin produced.
	receiverMgr := routing.NewManager(receiverID)
	receiver := NewFlooder(DefaultFloodConfig(), receiverID, receiverMgr, newMockPeerSender())
	defer receiver.Stop()

	accepted := 0
	for _, frame := range originSender.GetMessages(peerID) {
		adv, err := protocol.DecodeRouteAdvertise(frame.Payload)
		if err != nil {
			t.Fatalf("DecodeRouteAdvertise: %v", err)
		}
		if receiver.HandleRouteAdvertise(originID, adv.OriginAgent, adv.OriginDisplayName,
			adv.Sequence, adv.Routes, adv.Comments, adv.EncPath, adv.SeenBy) {
			accepted++
		}
	}
	if accepted < 2 {
		t.Fatalf("receiver accepted %d advertisements; the rest were dropped as duplicates", accepted)
	}

	// Spot-check routes drawn from the first, middle and last chunk.
	for _, probe := range []string{"10.0.0.1", "10.1.44.1", "10.2.87.1"} {
		if route := receiverMgr.Lookup(net.ParseIP(probe)); route == nil {
			t.Errorf("route covering %s never reached the receiver", probe)
		}
	}
}

// Routes stored under our own origin are never expired by CleanupStaleRoutes,
// so a peer must not be able to install one: it would be unwithdrawable and
// unageable for the life of the process.
func TestFlooder_RejectsAdvertisementClaimingLocalOrigin(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()

	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{{
		AddressFamily: protocol.AddrFamilyIPv4,
		PrefixLength:  0,
		Prefix:        []byte{0, 0, 0, 0},
		Metric:        1,
	}}

	// The peer forges our own AgentID as the origin.
	if f.HandleRouteAdvertise(peerID, localID, "", 999, routes, nil, nil, nil) {
		t.Error("advertisement claiming our own origin was accepted")
	}

	if route := routeMgr.Lookup(net.ParseIP("8.8.8.8")); route != nil {
		t.Error("forged self-origin route was installed in the routing table")
	}
}

// A withdraw carrying the same (origin, sequence) pair as the advertisement
// it revokes must still be processed: the seen cache namespaces withdraws
// separately from advertisements. Without the split, a neighbour re-flooding
// an origin's withdraw after the origin's link died would be swallowed by the
// earlier advertisement's seen-cache entry.
func TestFlooder_HandleRouteWithdraw_SameSequenceAsAdvertise(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{{
		AddressFamily: protocol.AddrFamilyIPv4,
		PrefixLength:  8,
		Prefix:        []byte{10, 0, 0, 0},
		Metric:        10,
	}}

	if !f.HandleRouteAdvertise(peerID, peerID, "", 5, routes, nil, nil, nil) {
		t.Fatal("advertisement should be accepted")
	}
	if routeMgr.Size() != 1 {
		t.Fatalf("Size after advertise = %d, want 1", routeMgr.Size())
	}

	// Same origin and sequence as the advertisement.
	if !f.HandleRouteWithdraw(peerID, peerID, 5, routes, nil) {
		t.Error("withdraw with the advertisement's sequence should be accepted")
	}
	if routeMgr.Size() != 0 {
		t.Errorf("Size after withdraw = %d, want 0", routeMgr.Size())
	}
}

// A withdraw older than the installed route's sequence must not remove the
// route (a stale withdraw traveling a slow path would otherwise kill a fresh
// re-advertisement), though it is still a new frame worth re-flooding.
func TestFlooder_HandleRouteWithdraw_StaleSequence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{{
		AddressFamily: protocol.AddrFamilyIPv4,
		PrefixLength:  8,
		Prefix:        []byte{10, 0, 0, 0},
		Metric:        10,
	}}

	if !f.HandleRouteAdvertise(peerID, peerID, "", 5, routes, nil, nil, nil) {
		t.Fatal("advertisement should be accepted")
	}

	// Stale withdraw: sequence 4 < installed 5.
	if !f.HandleRouteWithdraw(peerID, peerID, 4, routes, nil) {
		t.Error("stale withdraw is still a new frame and should be accepted for flooding")
	}
	if routeMgr.Size() != 1 {
		t.Errorf("Size after stale withdraw = %d, want 1 (route must survive)", routeMgr.Size())
	}

	// Fresh withdraw removes it.
	f.HandleRouteWithdraw(peerID, peerID, 6, routes, nil)
	if routeMgr.Size() != 0 {
		t.Errorf("Size after fresh withdraw = %d, want 0", routeMgr.Size())
	}
}

// Agent presence routes must be withdrawable like any other family.
func TestFlooder_HandleRouteWithdraw_AgentPresence(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()

	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	routes := []protocol.Route{{
		AddressFamily: protocol.AddrFamilyAgent,
		PrefixLength:  0,
		Prefix:        protocol.EncodeAgentPrefix(originID),
		Metric:        0,
	}}

	if !f.HandleRouteAdvertise(peerID, originID, "", 3, routes, nil, nil, nil) {
		t.Fatal("advertisement should be accepted")
	}
	if routeMgr.LookupAgent(originID) == nil {
		t.Fatal("agent presence route should be installed")
	}

	if !f.HandleRouteWithdraw(peerID, originID, 3, routes, nil) {
		t.Error("agent presence withdraw should be accepted")
	}
	if routeMgr.LookupAgent(originID) != nil {
		t.Error("agent presence route should be removed after withdraw")
	}
}

// Graceful shutdown withdraws every local route family, including the agent
// presence record, so peers drop this agent immediately instead of waiting
// for route TTL expiry.
func TestFlooder_WithdrawLocalRoutes_AllFamilies(t *testing.T) {
	localID, _ := identity.NewAgentID()
	peerID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(peerID)

	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	routeMgr.AddLocalRoute(routing.MustParseCIDR("10.0.0.0/8"), 10, "")
	routeMgr.AddLocalDomainRoute("example.com", 5)
	routeMgr.AddLocalForwardRoute("ssh", "10.0.0.1:22", 5)

	f.WithdrawLocalRoutes()

	msgs := sender.GetMessages(peerID)
	if len(msgs) != 1 {
		t.Fatalf("Should send 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != protocol.FrameRouteWithdraw {
		t.Fatalf("Frame type = 0x%02x, want ROUTE_WITHDRAW", msgs[0].Type)
	}

	withdraw, err := protocol.DecodeRouteWithdraw(msgs[0].Payload)
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw: %v", err)
	}
	if withdraw.OriginAgent != localID {
		t.Error("withdraw origin should be the local agent")
	}

	families := make(map[uint8]bool)
	for _, r := range withdraw.Routes {
		families[r.AddressFamily] = true
	}
	for _, fam := range []uint8{
		protocol.AddrFamilyIPv4,
		protocol.AddrFamilyDomain,
		protocol.AddrFamilyForward,
		protocol.AddrFamilyAgent,
	} {
		if !families[fam] {
			t.Errorf("withdraw missing address family 0x%02x", fam)
		}
	}

	// The presence record must name this agent.
	foundPresence := false
	for _, r := range withdraw.Routes {
		if r.AddressFamily == protocol.AddrFamilyAgent &&
			protocol.DecodeAgentPrefix(r.Prefix) == localID {
			foundPresence = true
		}
	}
	if !foundPresence {
		t.Error("withdraw missing agent presence record for local agent")
	}
}

// When a peer disconnects, the routes that died with it are re-flooded as
// withdrawals under the origin's own identity and the sequence they were
// learned under, so receivers drop them immediately.
func TestFlooder_FloodWithdrawnRoutes(t *testing.T) {
	localID, _ := identity.NewAgentID()
	deadPeer, _ := identity.NewAgentID()
	receiver, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(receiver)

	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	path := []identity.AgentID{deadPeer, originID}
	routeMgr.ProcessRouteAdvertise(deadPeer, originID, 7,
		[]routing.RouteEntry{{Network: routing.MustParseCIDR("10.0.0.0/8"), Metric: 1}}, path, nil)
	routeMgr.ProcessDomainRouteAdvertise(deadPeer, originID, 7,
		[]routing.DomainRouteEntry{{Pattern: "example.com"}}, path, nil)
	routeMgr.ProcessForwardRouteAdvertise(deadPeer, originID, 7,
		[]routing.ForwardRouteEntry{{Key: "ssh", Target: "10.0.0.1:22"}}, path, nil)
	routeMgr.ProcessAgentRouteAdvertise(deadPeer, originID, 7, originID, path, nil, 1)

	removedCIDR := routeMgr.HandlePeerDisconnect(deadPeer)
	removedDomain := routeMgr.HandlePeerDisconnectDomain(deadPeer)
	removedForward := routeMgr.HandlePeerDisconnectForward(deadPeer)
	removedAgent := routeMgr.HandlePeerDisconnectAgent(deadPeer)

	f.FloodWithdrawnRoutes(removedCIDR, removedDomain, removedForward, removedAgent)

	msgs := sender.GetMessages(receiver)
	if len(msgs) != 1 {
		t.Fatalf("Should send 1 withdraw (single origin+sequence group), got %d", len(msgs))
	}
	if msgs[0].Type != protocol.FrameRouteWithdraw {
		t.Fatalf("Frame type = 0x%02x, want ROUTE_WITHDRAW", msgs[0].Type)
	}

	withdraw, err := protocol.DecodeRouteWithdraw(msgs[0].Payload)
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw: %v", err)
	}
	if withdraw.OriginAgent != originID {
		t.Error("withdraw must carry the route origin's identity, not ours")
	}
	if withdraw.Sequence != 7 {
		t.Errorf("withdraw sequence = %d, want 7 (the sequence the routes were learned under)", withdraw.Sequence)
	}

	families := make(map[uint8]bool)
	for _, r := range withdraw.Routes {
		families[r.AddressFamily] = true
	}
	for _, fam := range []uint8{
		protocol.AddrFamilyIPv4,
		protocol.AddrFamilyDomain,
		protocol.AddrFamilyForward,
		protocol.AddrFamilyAgent,
	} {
		if !families[fam] {
			t.Errorf("withdraw missing address family 0x%02x", fam)
		}
	}
}

// Routes still reachable through another next hop must not be withdrawn:
// receivers would drop a route that is still usable via us.
func TestFlooder_FloodWithdrawnRoutes_SkipsReachable(t *testing.T) {
	localID, _ := identity.NewAgentID()
	deadPeer, _ := identity.NewAgentID()
	otherPeer, _ := identity.NewAgentID()
	receiver, _ := identity.NewAgentID()
	originID, _ := identity.NewAgentID()
	routeMgr := routing.NewManager(localID)
	sender := newMockPeerSender()
	sender.AddPeer(receiver)

	f := NewFlooder(DefaultFloodConfig(), localID, routeMgr, sender)
	defer f.Stop()

	// The agent table holds one entry per (origin, next hop): originID's
	// presence route arrives via both peers, and only the dead peer's entry
	// is removed on disconnect.
	routeMgr.ProcessAgentRouteAdvertise(deadPeer, originID, 7, originID,
		[]identity.AgentID{deadPeer, originID}, nil, 1)
	routeMgr.ProcessAgentRouteAdvertise(otherPeer, originID, 7, originID,
		[]identity.AgentID{otherPeer, originID}, nil, 2)

	removedAgent := routeMgr.HandlePeerDisconnectAgent(deadPeer)
	if len(removedAgent) != 1 {
		t.Fatalf("HandlePeerDisconnectAgent removed %d routes, want 1", len(removedAgent))
	}

	f.FloodWithdrawnRoutes(nil, nil, nil, removedAgent)

	if msgs := sender.GetMessages(receiver); len(msgs) != 0 {
		t.Errorf("no withdraw expected for a still-reachable route, got %d frames", len(msgs))
	}
}
