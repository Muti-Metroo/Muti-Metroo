package config

// redactedPlaceholder is the sentinel that replaces every secret value when
// emitting a config via `muti-metroo config show`. The "<set>" form is preserved
// for fields that were configured (so an operator can tell from the output
// whether a secret is present), while empty fields stay empty.
const redactedPlaceholder = "<redacted>"

// Redacted returns a deep copy of c with every secret-bearing field replaced
// by a non-sensitive placeholder. Used by `muti-metroo config show` so operators
// can dump effective configuration without leaking private keys, bearer
// tokens, password hashes, or proxy credentials into logs / scrollback.
//
// Fields that were never configured stay as the zero value; fields that
// held a secret are replaced with "<redacted>" so the output still answers
// the question "is this secret set?".
//
// If c is nil, returns nil.
func Redacted(c *Config) *Config {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Listeners = append([]ListenerConfig(nil), c.Listeners...)
	cp.Peers = append([]PeerConfig(nil), c.Peers...)

	// Identity keypair.
	cp.Agent.PrivateKey = redactIfSet(c.Agent.PrivateKey)

	// Global TLS material.
	cp.TLS.Key = redactIfSet(c.TLS.Key)
	cp.TLS.KeyPEM = redactIfSet(c.TLS.KeyPEM)

	// Per-listener TLS overrides.
	for i := range cp.Listeners {
		cp.Listeners[i].TLS.Key = redactIfSet(cp.Listeners[i].TLS.Key)
		cp.Listeners[i].TLS.KeyPEM = redactIfSet(cp.Listeners[i].TLS.KeyPEM)
	}

	// Per-peer TLS overrides + proxy credentials.
	for i := range cp.Peers {
		cp.Peers[i].TLS.Key = redactIfSet(cp.Peers[i].TLS.Key)
		cp.Peers[i].TLS.KeyPEM = redactIfSet(cp.Peers[i].TLS.KeyPEM)
		cp.Peers[i].ProxyAuth.Password = redactIfSet(cp.Peers[i].ProxyAuth.Password)
	}

	// SOCKS5 user passwords + hashes.
	cp.SOCKS5 = c.SOCKS5
	if len(c.SOCKS5.Auth.Users) > 0 {
		cp.SOCKS5.Auth.Users = make([]SOCKS5UserConfig, len(c.SOCKS5.Auth.Users))
		for i, u := range c.SOCKS5.Auth.Users {
			cp.SOCKS5.Auth.Users[i] = u
			cp.SOCKS5.Auth.Users[i].Password = redactIfSet(u.Password)
			cp.SOCKS5.Auth.Users[i].PasswordHash = redactIfSet(u.PasswordHash)
		}
	}

	// HTTP API bearer token hash.
	cp.HTTP.TokenHash = redactIfSet(c.HTTP.TokenHash)

	// Mesh management secrets.
	cp.Management.PrivateKey = redactIfSet(c.Management.PrivateKey)
	cp.Management.SigningPrivateKey = redactIfSet(c.Management.SigningPrivateKey)

	return &cp
}

func redactIfSet(s string) string {
	if s == "" {
		return ""
	}
	return redactedPlaceholder
}
