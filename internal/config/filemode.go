package config

import "os"

// DefaultFileMode is the on-disk mode applied to any config file the CLI
// writes (the setup wizard today; future generators should reuse it).
//
// 0600 (owner read/write only) is required because a generated config can
// carry secrets: SOCKS5 plaintext passwords, the agent's X25519 private key,
// the management X25519 and Ed25519 private keys, TLS key_pem material, and
// bcrypt token hashes. On the shared-host deployments Muti Metroo targets, a
// world-readable config would expose all of those to any local user.
const DefaultFileMode os.FileMode = 0600
