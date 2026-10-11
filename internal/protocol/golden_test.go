package protocol

import (
	"encoding/hex"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/identity"
)

// Golden-bytes tests pin the exact wire encoding of the frame families whose
// serializers share helpers (openWire/ackWire/errWire, sleepWakeWire, and the
// route list helpers). The hex strings below were produced by the pre-dedup
// serializers; a change in encoded bytes is a wire-format break.

func goldenAgentIDs() (origin, peer identity.AgentID) {
	for i := range origin {
		origin[i] = byte(i + 1)
	}
	for i := range peer {
		peer[i] = byte(0xA0 + i)
	}
	return origin, peer
}

func goldenEphemeral() [EphemeralKeySize]byte {
	var eph [EphemeralKeySize]byte
	for i := range eph {
		eph[i] = byte(0x50 + i)
	}
	return eph
}

func assertGolden(t *testing.T, name string, got []byte, wantHex string) {
	t.Helper()
	if hex.EncodeToString(got) != wantHex {
		t.Errorf("%s encoding changed\n got: %x\nwant: %s", name, got, wantHex)
	}
}

func TestGoldenStreamOpen(t *testing.T) {
	_, peer := goldenAgentIDs()
	f := &StreamOpen{
		RequestID:       0x0102030405060708,
		AddressType:     AddrTypeIPv4,
		Address:         []byte{10, 1, 2, 3},
		Port:            8080,
		TTL:             15,
		RemainingPath:   []identity.AgentID{peer},
		EphemeralPubKey: goldenEphemeral(),
	}
	assertGolden(t, "StreamOpen", f.Encode(),
		"0102030405060708010a0102031f900f01a0a1a2a3a4a5a6a7a8a9aaabacadaeaf"+
			"505152535455565758595a5b5c5d5e5f606162636465666768696a6b6c6d6e6f")

	// UDPOpen shares openWire with StreamOpen; identical fields must produce
	// identical bytes.
	u := &UDPOpen{
		RequestID:       f.RequestID,
		AddressType:     f.AddressType,
		Address:         f.Address,
		Port:            f.Port,
		TTL:             f.TTL,
		RemainingPath:   f.RemainingPath,
		EphemeralPubKey: f.EphemeralPubKey,
	}
	assertGolden(t, "UDPOpen==StreamOpen", u.Encode(), hex.EncodeToString(f.Encode()))

	// Round-trip through the shared decoder.
	dec, err := DecodeStreamOpen(f.Encode())
	if err != nil {
		t.Fatalf("DecodeStreamOpen: %v", err)
	}
	if dec.RequestID != f.RequestID || dec.Port != f.Port || dec.TTL != f.TTL {
		t.Errorf("round-trip mismatch: %+v", dec)
	}
}

func TestGoldenStreamOpenAck(t *testing.T) {
	ack := &StreamOpenAck{
		RequestID:       0x0102030405060708,
		BoundAddrType:   AddrTypeIPv4,
		BoundAddr:       []byte{192, 168, 1, 1},
		BoundPort:       443,
		EphemeralPubKey: goldenEphemeral(),
	}
	assertGolden(t, "StreamOpenAck", ack.Encode(),
		"010203040506070801c0a8010101bb505152535455565758595a5b5c5d5e5f"+
			"606162636465666768696a6b6c6d6e6f")

	// UDPOpenAck shares ackWire with StreamOpenAck.
	u := &UDPOpenAck{
		RequestID:       ack.RequestID,
		BoundAddrType:   ack.BoundAddrType,
		BoundAddr:       ack.BoundAddr,
		BoundPort:       ack.BoundPort,
		EphemeralPubKey: ack.EphemeralPubKey,
	}
	assertGolden(t, "UDPOpenAck==StreamOpenAck", u.Encode(), hex.EncodeToString(ack.Encode()))
}

func TestGoldenStreamOpenErr(t *testing.T) {
	e := &StreamOpenErr{RequestID: 7, ErrorCode: 0x0203, Message: "denied"}
	assertGolden(t, "StreamOpenErr", e.Encode(), "000000000000000702030664656e696564")

	// UDPOpenErr and ICMPOpenErr share errWire with StreamOpenErr.
	u := &UDPOpenErr{RequestID: e.RequestID, ErrorCode: e.ErrorCode, Message: e.Message}
	assertGolden(t, "UDPOpenErr==StreamOpenErr", u.Encode(), hex.EncodeToString(e.Encode()))
	i := &ICMPOpenErr{RequestID: e.RequestID, ErrorCode: e.ErrorCode, Message: e.Message}
	assertGolden(t, "ICMPOpenErr==StreamOpenErr", i.Encode(), hex.EncodeToString(e.Encode()))
}

func TestGoldenSleepWakeCommand(t *testing.T) {
	origin, peer := goldenAgentIDs()
	sleep := &SleepCommand{
		OriginAgent: origin,
		CommandID:   42,
		Timestamp:   1700000000,
		SeenBy:      []identity.AgentID{peer},
	}
	want := "0102030405060708090a0b0c0d0e0f10" + // origin
		"000000000000002a" + // command ID
		"000000006553f100" + // timestamp
		"0000000000000000000000000000000000000000000000000000000000000000" + // signature (zeros)
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"01" + // seenBy count
		"a0a1a2a3a4a5a6a7a8a9aaabacadaeaf" // seenBy[0]
	assertGolden(t, "SleepCommand", sleep.Encode(), want)

	// WakeCommand shares sleepWakeWire with SleepCommand; identical fields
	// must produce identical bytes.
	wake := &WakeCommand{
		OriginAgent: sleep.OriginAgent,
		CommandID:   sleep.CommandID,
		Timestamp:   sleep.Timestamp,
		SeenBy:      sleep.SeenBy,
	}
	assertGolden(t, "WakeCommand==SleepCommand", wake.Encode(), want)

	if sleep.SignableBytes()[0] != 0x01 || len(sleep.SignableBytes()) != 32 {
		t.Errorf("SignableBytes = %x", sleep.SignableBytes())
	}
}

func TestGoldenRouteAdvertise(t *testing.T) {
	origin, peer := goldenAgentIDs()
	adv := &RouteAdvertise{
		OriginAgent:       origin,
		OriginDisplayName: "orig",
		Sequence:          9,
		Routes: []Route{
			{AddressFamily: AddrFamilyIPv4, PrefixLength: 24, Prefix: []byte{10, 0, 0, 0}, Metric: 3},
			{AddressFamily: AddrFamilyDomain, PrefixLength: 1, Prefix: EncodeDomainPrefix("*.example.com"), Metric: 4},
			{AddressFamily: AddrFamilyForward, Prefix: EncodeForwardKeyWithTarget("ssh", "10.0.0.1:22"), Metric: 5},
			{AddressFamily: AddrFamilyAgent, Prefix: EncodeAgentPrefix(origin), Metric: 0},
		},
		Path:     []identity.AgentID{origin, peer},
		SeenBy:   []identity.AgentID{origin},
		Comments: []string{"note", "", "", ""},
	}
	want := "0102030405060708090a0b0c0d0e0f10" + // origin
		"046f726967" + // display name "orig"
		"0000000000000009" + // sequence
		"04" + // route count
		"01180a0000000003" + // ipv4 10.0.0.0/24 metric 3
		"0301" + "0d2a2e6578616d706c652e636f6d" + "0004" + // domain *.example.com metric 4
		"0400" + "037373680b31302e302e302e313a3232" + "0005" + // forward ssh -> 10.0.0.1:22 metric 5
		"0500" + "0102030405060708090a0b0c0d0e0f10" + "0000" + // agent presence metric 0
		"00" + "0021" + "02" + "0102030405060708090a0b0c0d0e0f10" + "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf" + // EncPath: plaintext, len 33, 2-agent path
		"01" + "0102030405060708090a0b0c0d0e0f10" + // seenBy
		"04" + "046e6f7465" + "00" + "00" + "00" // comments: "note","","",""
	assertGolden(t, "RouteAdvertise", adv.Encode(), want)

	// Round-trip.
	dec, err := DecodeRouteAdvertise(adv.Encode())
	if err != nil {
		t.Fatalf("DecodeRouteAdvertise: %v", err)
	}
	if len(dec.Routes) != 4 || dec.Routes[1].Metric != 4 || len(dec.Path) != 2 {
		t.Errorf("round-trip mismatch: %+v", dec)
	}
	if len(dec.Comments) != 4 || dec.Comments[0] != "note" {
		t.Errorf("comments round-trip mismatch: %q", dec.Comments)
	}
}

func TestGoldenRouteWithdraw(t *testing.T) {
	origin, peer := goldenAgentIDs()
	wd := &RouteWithdraw{
		OriginAgent: origin,
		Sequence:    10,
		Routes: []Route{
			{AddressFamily: AddrFamilyIPv4, PrefixLength: 24, Prefix: []byte{10, 0, 0, 0}, Metric: 3},
		},
		SeenBy: []identity.AgentID{peer},
	}
	want := "0102030405060708090a0b0c0d0e0f10" + // origin
		"000000000000000a" + // sequence
		"01" + // route count
		"01180a0000000003" + // ipv4 10.0.0.0/24 metric 3
		"01" + "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf" // seenBy
	assertGolden(t, "RouteWithdraw", wd.Encode(), want)

	// The route list layout is shared with RouteAdvertise: the same route
	// must decode identically from a withdraw.
	dec, err := DecodeRouteWithdraw(wd.Encode())
	if err != nil {
		t.Fatalf("DecodeRouteWithdraw: %v", err)
	}
	if len(dec.Routes) != 1 || dec.Routes[0].PrefixLength != 24 || dec.Routes[0].Metric != 3 {
		t.Errorf("round-trip mismatch: %+v", dec)
	}
}

func TestGoldenCloseReasons(t *testing.T) {
	// UDPClose and ICMPClose share the single-reason-byte layout.
	u := &UDPClose{Reason: UDPCloseTimeout}
	i := &ICMPClose{Reason: 1}
	assertGolden(t, "UDPClose", u.Encode(), "01")
	assertGolden(t, "ICMPClose==UDPClose", i.Encode(), hex.EncodeToString(u.Encode()))

	if _, err := DecodeUDPClose(nil); err == nil {
		t.Error("DecodeUDPClose(nil) should fail")
	}
	if _, err := DecodeICMPClose(nil); err == nil {
		t.Error("DecodeICMPClose(nil) should fail")
	}
}
