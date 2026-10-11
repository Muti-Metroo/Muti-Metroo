package config

import "testing"

func TestRedactedNil(t *testing.T) {
	if got := Redacted(nil); got != nil {
		t.Errorf("Redacted(nil) = %v, want nil", got)
	}
}

func TestRedactedReplacesSetSecrets(t *testing.T) {
	c := &Config{
		Agent: AgentConfig{
			ID:         "abc",
			PrivateKey: "deadbeef",
		},
		TLS: GlobalTLSConfig{
			Key:    "/etc/muti-metroo/agent.key",
			KeyPEM: "-----BEGIN PRIVATE KEY-----\n...",
		},
		Peers: []PeerConfig{
			{
				ID:        "peer1",
				Transport: "quic",
				Address:   "host:4433",
				ProxyAuth: ProxyAuth{Username: "u", Password: "p"},
				TLS:       TLSConfig{Key: "/k", KeyPEM: "PEM"},
			},
		},
		SOCKS5: SOCKS5Config{
			Auth: SOCKS5AuthConfig{
				Users: []SOCKS5UserConfig{
					{Username: "u1", Password: "plaintext", PasswordHash: "$2a$..."},
					{Username: "u2"}, // empty creds stay empty
				},
			},
		},
		HTTP: HTTPConfig{
			TokenHash: "$2a$10$...",
		},
		Management: ManagementConfig{
			PrivateKey:        "deadbeef" + "deadbeef",
			SigningPrivateKey: "0102030405",
		},
	}
	r := Redacted(c)
	if r == nil {
		t.Fatal("Redacted returned nil for non-nil input")
	}
	// Non-secret fields preserved
	if r.Agent.ID != "abc" {
		t.Errorf("non-secret Agent.ID lost: got %q", r.Agent.ID)
	}
	if r.Peers[0].Address != "host:4433" {
		t.Errorf("non-secret peer.Address lost: got %q", r.Peers[0].Address)
	}
	if r.SOCKS5.Auth.Users[0].Username != "u1" {
		t.Errorf("non-secret socks user username lost: got %q", r.SOCKS5.Auth.Users[0].Username)
	}
	// Set secrets become "<redacted>"
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"Agent.PrivateKey", r.Agent.PrivateKey},
		{"TLS.Key", r.TLS.Key},
		{"TLS.KeyPEM", r.TLS.KeyPEM},
		{"Peers[0].ProxyAuth.Password", r.Peers[0].ProxyAuth.Password},
		{"Peers[0].TLS.Key", r.Peers[0].TLS.Key},
		{"Peers[0].TLS.KeyPEM", r.Peers[0].TLS.KeyPEM},
		{"SOCKS5.Users[0].Password", r.SOCKS5.Auth.Users[0].Password},
		{"SOCKS5.Users[0].PasswordHash", r.SOCKS5.Auth.Users[0].PasswordHash},
		{"HTTP.TokenHash", r.HTTP.TokenHash},
		{"Management.PrivateKey", r.Management.PrivateKey},
		{"Management.SigningPrivateKey", r.Management.SigningPrivateKey},
	} {
		if tc.got != redactedPlaceholder {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, redactedPlaceholder)
		}
	}
	// Empty creds stay empty (so the diff against defaults stays clean)
	if r.SOCKS5.Auth.Users[1].Password != "" || r.SOCKS5.Auth.Users[1].PasswordHash != "" {
		t.Errorf("empty creds got rewritten: %#v", r.SOCKS5.Auth.Users[1])
	}
	// Non-secret peer username should be untouched
	if r.Peers[0].ProxyAuth.Username != "u" {
		t.Errorf("non-secret proxy username lost: got %q", r.Peers[0].ProxyAuth.Username)
	}
	// Original config must NOT be mutated.
	if c.Agent.PrivateKey != "deadbeef" {
		t.Errorf("Redacted mutated input config: Agent.PrivateKey = %q", c.Agent.PrivateKey)
	}
	if c.SOCKS5.Auth.Users[0].PasswordHash != "$2a$..." {
		t.Errorf("Redacted mutated SOCKS5 user: %q", c.SOCKS5.Auth.Users[0].PasswordHash)
	}
}
