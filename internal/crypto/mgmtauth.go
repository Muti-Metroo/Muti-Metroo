package crypto

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/postalsys/muti-metroo/internal/identity"
)

// ManagementTimestampWindow is the maximum tolerated clock skew between
// signer and verifier for signed management operations (shell, file
// transfer, dynamic config mutations). Mirrors the default used by
// sleep/wake flooding.
const ManagementTimestampWindow = 5 * time.Minute

// Operation tags bound into the signed payload to prevent cross-operation
// replay: a signature captured for one operation cannot be reused for
// another. Tags are stable ASCII literals.
const (
	OpShell                 = "shell"
	OpFile                  = "file"
	OpRouteAdd              = "route.add"
	OpRouteRemove           = "route.remove"
	OpRouteCommentSet       = "route.comment_set"
	OpRouteSourceIPSet      = "route.source_ip_set"
	OpRouteInterfaceSet     = "route.interface_set"
	OpPeerAdd               = "peer.add"
	OpPeerRemove            = "peer.remove"
	OpForwardAdd            = "forward.add"
	OpForwardRemove         = "forward.remove"
	OpForwardEndpointAdd    = "forward_endpoint.add"
	OpForwardEndpointRemove = "forward_endpoint.remove"
	OpListenerAdd           = "listener.add"
	OpListenerRemove        = "listener.remove"
	OpDisplayNameSet        = "display_name.set"
	OpHostAdd               = "host.add"
	OpHostRemove            = "host.remove"
	OpStop                  = "stop"
)

// AuthEnvelope carries an Ed25519 signature over
// (origin || target || timestamp_be || opTag || bodyHash) together with
// the fields needed to reconstruct that signable. Origin and target are
// AgentIDs so a captured signature cannot be replayed against a different
// agent; bodyHash binds mutation payloads so "add X" cannot be replayed
// as "remove Y".
type AuthEnvelope struct {
	OriginAgent identity.AgentID `json:"origin_agent"`
	TargetAgent identity.AgentID `json:"target_agent"`
	Timestamp   int64            `json:"timestamp"`
	Signature   []byte           `json:"signature"`
}

// Auth error sentinels. Callers distinguish failure modes via errors.Is
// so HTTP handlers can map to 401/403 and logs can stay machine-readable.
var (
	ErrAuthMissing      = errors.New("management signature missing")
	ErrAuthWrongTarget  = errors.New("management signature target mismatch")
	ErrAuthStale        = errors.New("management signature outside timestamp window")
	ErrAuthBadSignature = errors.New("management signature verification failed")
)

// ManagementSignable returns the canonical bytes covered by the envelope
// signature. bodyHash may be nil -- shell and file-transfer bind only to
// the (origin, target, timestamp, opTag) tuple since target-side policy
// (command whitelist, allowed paths, size limits) constrains what the
// operation can actually do.
func ManagementSignable(env AuthEnvelope, opTag string, bodyHash []byte) []byte {
	out := make([]byte, 0, identity.IDSize+identity.IDSize+8+len(opTag)+len(bodyHash))
	out = append(out, env.OriginAgent[:]...)
	out = append(out, env.TargetAgent[:]...)
	tsBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(tsBuf, uint64(env.Timestamp))
	out = append(out, tsBuf...)
	out = append(out, []byte(opTag)...)
	out = append(out, bodyHash...)
	return out
}

// SignManagement builds a fresh envelope stamped with the current time
// and signed for the given operation. privateKey is the 64-byte Ed25519
// private key (typically config.GetSigningPrivateKey()).
func SignManagement(privateKey [Ed25519PrivateKeySize]byte, origin, target identity.AgentID, opTag string, bodyHash []byte) AuthEnvelope {
	env := AuthEnvelope{
		OriginAgent: origin,
		TargetAgent: target,
		Timestamp:   time.Now().Unix(),
	}
	sig := Sign(privateKey, ManagementSignable(env, opTag, bodyHash))
	env.Signature = append([]byte(nil), sig[:]...)
	return env
}

// VerifyManagement checks an envelope against the verifier's configured
// public key. Returns nil on success or one of the ErrAuth* sentinels
// (possibly wrapped) describing the failure.
func VerifyManagement(publicKey [Ed25519PublicKeySize]byte, selfID identity.AgentID, env AuthEnvelope, opTag string, bodyHash []byte) error {
	if len(env.Signature) != Ed25519SignatureSize {
		return ErrAuthMissing
	}
	if env.TargetAgent != selfID {
		return ErrAuthWrongTarget
	}
	ts := time.Unix(env.Timestamp, 0)
	diff := time.Since(ts)
	if diff < 0 {
		diff = -diff
	}
	if diff > ManagementTimestampWindow {
		return fmt.Errorf("%w (%v off, max %v)", ErrAuthStale, diff, ManagementTimestampWindow)
	}
	var sig [Ed25519SignatureSize]byte
	copy(sig[:], env.Signature)
	if !Verify(publicKey, ManagementSignable(env, opTag, bodyHash), sig) {
		return ErrAuthBadSignature
	}
	return nil
}

// HashBody returns sha256(b). Convenience for mutation handlers binding
// the JSON request body (with the auth envelope stripped) into the
// signature.
func HashBody(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
