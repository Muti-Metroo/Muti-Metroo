// Package apiclient provides helpers for addressing a Muti Metroo agent's HTTP API.
//
// The CLI accepts either a bare host:port (defaulting to http) or a full
// http(s):// URL with an optional path prefix. Endpoint captures that choice
// once so downstream code does not have to re-parse or carry a scheme flag
// around.
package apiclient

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Endpoint is a parsed agent API address.
type Endpoint struct {
	Scheme     string // "http" or "https"
	Host       string // host or host:port
	PathPrefix string // "" or "/prefix" (no trailing slash)
}

// Parse turns the -a/--agent flag value into an Endpoint.
//
// Accepted forms:
//
//	host:port                       -> http, no prefix
//	http://host[:port][/prefix]     -> http
//	https://host[:port][/prefix]    -> https (Go fills 443 when port omitted)
//
// Anything else (other schemes, embedded credentials, query strings, fragments,
// empty input, bare host with a path but no scheme) is rejected with an error
// that names the offending input.
func Parse(raw string) (Endpoint, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Endpoint{}, errors.New("agent address is empty")
	}

	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return Endpoint{}, fmt.Errorf("invalid agent URL %q: %w", raw, err)
		}
		if u.Host == "" {
			return Endpoint{}, fmt.Errorf("agent URL %q has no host", raw)
		}
		if u.User != nil {
			return Endpoint{}, fmt.Errorf("agent URL %q must not include credentials", raw)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return Endpoint{}, fmt.Errorf("agent URL %q must not include a query or fragment", raw)
		}
		return Endpoint{
			Scheme:     u.Scheme,
			Host:       u.Host,
			PathPrefix: strings.TrimRight(u.Path, "/"),
		}, nil
	}

	if strings.Contains(s, "://") {
		return Endpoint{}, fmt.Errorf("agent address %q uses an unsupported scheme (expected http or https)", raw)
	}

	if strings.ContainsAny(s, "/?#") {
		return Endpoint{}, fmt.Errorf("agent address %q is not valid (use host:port or http(s)://host[:port][/prefix])", raw)
	}

	return Endpoint{Scheme: "http", Host: s}, nil
}

// HTTPURL builds an http(s) URL for the given API path. A leading slash is
// added if missing so callers can pass either "/agents" or "agents".
func (e Endpoint) HTTPURL(path string) string {
	return e.Scheme + "://" + e.Host + e.PathPrefix + normalizePath(path)
}

// WSURL builds a ws(s) URL for the given API path, deriving the scheme from
// the endpoint's http(s) scheme.
func (e Endpoint) WSURL(path string) string {
	scheme := "ws"
	if e.Scheme == "https" {
		scheme = "wss"
	}
	return scheme + "://" + e.Host + e.PathPrefix + normalizePath(path)
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}
