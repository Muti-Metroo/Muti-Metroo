// Package socks5 implements a SOCKS5 proxy server for Muti Metroo.
package socks5

import (
	"crypto/subtle"
	"errors"
	"io"

	"golang.org/x/crypto/bcrypt"
)

// Authentication method constants per RFC 1928.
const (
	AuthMethodNoAuth       = 0x00
	AuthMethodGSSAPI       = 0x01
	AuthMethodUserPass     = 0x02
	AuthMethodNoAcceptable = 0xFF
)

// Auth status for username/password auth (RFC 1929).
const (
	AuthStatusSuccess = 0x00
	AuthStatusFailure = 0x01
)

// Authenticator handles SOCKS5 authentication.
type Authenticator interface {
	// Authenticate performs authentication and returns the username if successful.
	Authenticate(reader io.Reader, writer io.Writer) (string, error)

	// GetMethod returns the authentication method code.
	GetMethod() byte
}

// NoAuthAuthenticator allows connections without authentication.
type NoAuthAuthenticator struct{}

// Authenticate always succeeds for no-auth.
func (a *NoAuthAuthenticator) Authenticate(reader io.Reader, writer io.Writer) (string, error) {
	return "", nil
}

// GetMethod returns the no-auth method.
func (a *NoAuthAuthenticator) GetMethod() byte {
	return AuthMethodNoAuth
}

// CredentialStore validates credentials.
type CredentialStore interface {
	Valid(username, password string) bool
}

// HashedCredentials stores username to bcrypt hash mappings.
// This is the recommended credential store for production use.
type HashedCredentials map[string]string

// Valid checks if the username/password combination is valid.
// Uses bcrypt comparison which is inherently constant-time.
func (h HashedCredentials) Valid(username, password string) bool {
	storedHash, ok := h[username]
	if !ok {
		// Perform a dummy bcrypt comparison to maintain constant time for invalid usernames
		// This uses a pre-computed hash for timing consistency
		bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)) == nil
}

// dummyHash is a pre-computed bcrypt hash used for timing attack prevention.
// It's compared against when the username doesn't exist.
var dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// StaticCredentials is a static credential store with plaintext passwords.
// Deprecated: Use HashedCredentials for production deployments.
type StaticCredentials map[string]string

// Valid checks if the username/password combination is valid.
// Uses constant-time comparison to prevent timing attacks.
// Deprecated: Use HashedCredentials for production deployments.
func (s StaticCredentials) Valid(username, password string) bool {
	storedPass, ok := s[username]
	if !ok {
		// Perform a dummy comparison to maintain constant time even for invalid usernames
		subtle.ConstantTimeCompare([]byte(password), []byte(password))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedPass), []byte(password)) == 1
}

// HashPassword creates a bcrypt hash of the password for SOCKS5 authentication.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// MustHashPassword creates a bcrypt hash and panics on error.
// For use in tests and initialization.
func MustHashPassword(password string) string {
	hash, err := HashPassword(password)
	if err != nil {
		panic(err)
	}
	return hash
}

// UserPassAuthenticator handles username/password authentication (RFC 1929).
type UserPassAuthenticator struct {
	Credentials CredentialStore
}

// NewUserPassAuthenticator creates a new username/password authenticator.
func NewUserPassAuthenticator(creds CredentialStore) *UserPassAuthenticator {
	return &UserPassAuthenticator{Credentials: creds}
}

// GetMethod returns the username/password method.
func (a *UserPassAuthenticator) GetMethod() byte {
	return AuthMethodUserPass
}

// Authenticate performs username/password authentication.
// Protocol (RFC 1929):
//
//	+----+------+----------+------+----------+
//	|VER | ULEN |  UNAME   | PLEN |  PASSWD  |
//	+----+------+----------+------+----------+
//	| 1  |  1   | 1 to 255 |  1   | 1 to 255 |
//	+----+------+----------+------+----------+
//
// Response:
//
//	+----+--------+
//	|VER | STATUS |
//	+----+--------+
//	| 1  |   1    |
//	+----+--------+
func (a *UserPassAuthenticator) Authenticate(reader io.Reader, writer io.Writer) (string, error) {
	// Read version (must be 0x01)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", err
	}

	if header[0] != 0x01 {
		return "", errors.New("unsupported auth version")
	}

	// Read username
	uLen := int(header[1])
	if uLen == 0 {
		return "", errors.New("username is empty")
	}

	username := make([]byte, uLen)
	if _, err := io.ReadFull(reader, username); err != nil {
		return "", err
	}

	// Read password length
	pLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(reader, pLenBuf); err != nil {
		return "", err
	}

	// Read password
	pLen := int(pLenBuf[0])
	password := make([]byte, pLen)
	if pLen > 0 {
		if _, err := io.ReadFull(reader, password); err != nil {
			return "", err
		}
	}

	// Validate credentials
	if !a.Credentials.Valid(string(username), string(password)) {
		// Send failure response
		writer.Write([]byte{0x01, AuthStatusFailure})
		return "", errors.New("authentication failed")
	}

	// Send success response
	_, err := writer.Write([]byte{0x01, AuthStatusSuccess})
	if err != nil {
		return "", err
	}

	return string(username), nil
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Enabled  bool
	Required bool
	// Users maps username to password (plaintext, deprecated).
	Users map[string]string
	// HashedUsers maps username to bcrypt password hash (recommended).
	HashedUsers map[string]string
}

// CreateAuthenticators creates authenticators based on config.
// If HashedUsers is provided, it takes precedence over Users.
func CreateAuthenticators(cfg AuthConfig) []Authenticator {
	var auths []Authenticator

	if cfg.Enabled {
		// Prefer hashed credentials if available
		if len(cfg.HashedUsers) > 0 {
			creds := HashedCredentials(cfg.HashedUsers)
			auths = append(auths, NewUserPassAuthenticator(creds))
		} else if len(cfg.Users) > 0 {
			// Fall back to plaintext credentials (deprecated)
			creds := StaticCredentials(cfg.Users)
			auths = append(auths, NewUserPassAuthenticator(creds))
		}
	}

	if !cfg.Required {
		auths = append(auths, &NoAuthAuthenticator{})
	}

	return auths
}
