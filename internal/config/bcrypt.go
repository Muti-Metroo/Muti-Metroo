package config

// DefaultBcryptCost is the work factor used by Muti Metroo when generating new
// bcrypt hashes for HTTP API tokens and SOCKS5 user passwords.
//
// 12 is the OWASP 2024 Password Storage Cheat Sheet recommendation; the
// stdlib bcrypt.DefaultCost is 10, which has been considered stale since
// roughly 2023. Bumping the cost only affects hash time at credential
// creation (in `muti-metroo hash` and the setup wizard); request-handling paths
// call bcrypt.CompareHashAndPassword which derives the cost from the
// stored hash itself, so existing hashes keep working.
const DefaultBcryptCost = 12
