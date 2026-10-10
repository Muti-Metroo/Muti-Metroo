package crypto

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

func mustAgentID(t *testing.T, hexStr string) identity.AgentID {
	t.Helper()
	id, err := identity.ParseAgentID(hexStr)
	if err != nil {
		t.Fatalf("parse agent id %q: %v", hexStr, err)
	}
	return id
}

func TestManagementSignable_Layout(t *testing.T) {
	origin := mustAgentID(t, "00112233445566778899aabbccddeeff")
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")
	env := AuthEnvelope{OriginAgent: origin, TargetAgent: target, Timestamp: 0x0102030405060708}

	got := ManagementSignable(env, "route.add", []byte{0xaa, 0xbb})

	want := make([]byte, 0, 16+16+8+len("route.add")+2)
	want = append(want, origin[:]...)
	want = append(want, target[:]...)
	tsBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBuf, uint64(env.Timestamp))
	want = append(want, tsBuf...)
	want = append(want, []byte("route.add")...)
	want = append(want, 0xaa, 0xbb)

	if !bytes.Equal(got, want) {
		t.Fatalf("signable mismatch\n got  %s\n want %s", hex.EncodeToString(got), hex.EncodeToString(want))
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	kp, err := GenerateSigningKeypair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	origin := mustAgentID(t, "00112233445566778899aabbccddeeff")
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")

	env := SignManagement(kp.PrivateKey, origin, target, OpShell, nil)
	if err := VerifyManagement(kp.PublicKey, target, env, OpShell, nil); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyMissingSignature(t *testing.T) {
	kp, _ := GenerateSigningKeypair()
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")
	env := AuthEnvelope{
		OriginAgent: mustAgentID(t, "00112233445566778899aabbccddeeff"),
		TargetAgent: target,
		Timestamp:   time.Now().Unix(),
	}
	err := VerifyManagement(kp.PublicKey, target, env, OpShell, nil)
	if !errors.Is(err, ErrAuthMissing) {
		t.Fatalf("want ErrAuthMissing, got %v", err)
	}
}

func TestVerifyWrongTarget(t *testing.T) {
	kp, _ := GenerateSigningKeypair()
	origin := mustAgentID(t, "00112233445566778899aabbccddeeff")
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")
	other := mustAgentID(t, "11111111111111112222222222222222")

	env := SignManagement(kp.PrivateKey, origin, target, OpShell, nil)
	err := VerifyManagement(kp.PublicKey, other, env, OpShell, nil)
	if !errors.Is(err, ErrAuthWrongTarget) {
		t.Fatalf("want ErrAuthWrongTarget, got %v", err)
	}
}

func TestVerifyStaleTimestamp(t *testing.T) {
	kp, _ := GenerateSigningKeypair()
	origin := mustAgentID(t, "00112233445566778899aabbccddeeff")
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")

	env := AuthEnvelope{
		OriginAgent: origin,
		TargetAgent: target,
		Timestamp:   time.Now().Add(-2 * ManagementTimestampWindow).Unix(),
	}
	sig := Sign(kp.PrivateKey, ManagementSignable(env, OpShell, nil))
	env.Signature = sig[:]

	err := VerifyManagement(kp.PublicKey, target, env, OpShell, nil)
	if !errors.Is(err, ErrAuthStale) {
		t.Fatalf("want ErrAuthStale, got %v", err)
	}
}

func TestVerifyTamperedBodyHash(t *testing.T) {
	kp, _ := GenerateSigningKeypair()
	origin := mustAgentID(t, "00112233445566778899aabbccddeeff")
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")

	env := SignManagement(kp.PrivateKey, origin, target, OpRouteAdd, HashBody([]byte(`{"cidr":"10.0.0.0/8"}`)))
	err := VerifyManagement(kp.PublicKey, target, env, OpRouteAdd, HashBody([]byte(`{"cidr":"0.0.0.0/0"}`)))
	if !errors.Is(err, ErrAuthBadSignature) {
		t.Fatalf("want ErrAuthBadSignature, got %v", err)
	}
}

func TestVerifyWrongOpTag(t *testing.T) {
	kp, _ := GenerateSigningKeypair()
	origin := mustAgentID(t, "00112233445566778899aabbccddeeff")
	target := mustAgentID(t, "ffeeddccbbaa99887766554433221100")

	env := SignManagement(kp.PrivateKey, origin, target, OpRouteAdd, nil)
	err := VerifyManagement(kp.PublicKey, target, env, OpRouteRemove, nil)
	if !errors.Is(err, ErrAuthBadSignature) {
		t.Fatalf("want ErrAuthBadSignature on op tag swap, got %v", err)
	}
}
