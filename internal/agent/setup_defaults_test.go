package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/postalsys/muti-metroo/internal/config"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// TestAgent_GetSetupDefaults_NoSOCKS5PlaintextLeak verifies that a plaintext
// SOCKS5 user password never appears in the setup-defaults payload. The
// response intentionally has no place for it; this pins that invariant so a
// future refactor cannot quietly start leaking plaintext credentials.
func TestAgent_GetSetupDefaults_NoSOCKS5PlaintextLeak(t *testing.T) {
	const plaintext = "hunter2-socks-secret"

	id, err := identity.NewAgentID()
	if err != nil {
		t.Fatalf("NewAgentID: %v", err)
	}

	cfg := &config.Config{
		Agent: config.AgentConfig{DisplayName: "test-agent"},
		SOCKS5: config.SOCKS5Config{
			Auth: config.SOCKS5AuthConfig{
				Enabled: true,
				Users: []config.SOCKS5UserConfig{
					{Username: "alice", Password: plaintext},
				},
			},
		},
	}

	a := &Agent{cfg: cfg, id: id}

	resp := a.GetSetupDefaults()

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), plaintext) {
		t.Errorf("response leaked SOCKS5 plaintext password: %s", raw)
	}
	if strings.Contains(string(raw), "alice") {
		t.Errorf("response leaked SOCKS5 username: %s", raw)
	}
}
