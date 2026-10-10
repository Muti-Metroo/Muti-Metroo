package agent

import (
	"net"
	"strings"

	"github.com/postalsys/muti-metroo/internal/health"
	"github.com/postalsys/muti-metroo/internal/sysinfo"
)

// GetSetupDefaults returns non-sensitive defaults for seeding a new agent's
// config via a wizard. Private keys, the TLS identity cert, and the HTTP
// bearer token are deliberately omitted.
func (a *Agent) GetSetupDefaults() health.SetupDefaultsResponse {
	cfg := a.cfg

	resp := health.SetupDefaultsResponse{
		AgentID:     a.id.String(),
		DisplayName: a.DisplayName(),
		Version:     sysinfo.Version,
	}

	if caPEM, err := cfg.TLS.GetCAPEM(); err == nil {
		resp.TLS.CAPEM = string(caPEM)
	}
	resp.TLS.Strict = cfg.TLS.Strict
	resp.TLS.MTLS = cfg.TLS.MTLS
	resp.TLS.SelfSigned = !cfg.TLS.HasCA() && !cfg.TLS.HasCert()

	resp.Management.Enabled = cfg.HasManagementKey()
	resp.Management.PublicKey = strings.ToLower(cfg.Management.PublicKey)
	resp.Management.HasPrivateKey = cfg.CanDecryptManagement()
	resp.Management.SigningPublicKey = strings.ToLower(cfg.Management.SigningPublicKey)
	resp.Management.HasSigningPrivateKey = cfg.CanSign()

	if len(cfg.Listeners) > 0 {
		ln := cfg.Listeners[0]
		resp.Listener = health.SetupDefaultsListener{
			Transport: ln.Transport,
			Address:   ln.Address,
			Path:      ln.Path,
			Plaintext: ln.PlainText,
		}
		resp.SuggestedPeer = health.SetupDefaultsSuggestedPeer{
			Address:    reachableListenerAddress(ln.Address),
			Transport:  ln.Transport,
			Path:       ln.Path,
			ExpectedID: a.id.String(),
		}
	}

	resp.HTTP = health.SetupDefaultsHTTP{
		Enabled:   cfg.HTTP.Enabled,
		TokenHash: cfg.HTTP.TokenHash,
	}
	resp.Shell = health.SetupDefaultsShell{
		Enabled:      cfg.Shell.Enabled,
		Whitelist:    append([]string(nil), cfg.Shell.Whitelist...),
		PasswordHash: cfg.Shell.PasswordHash,
	}
	resp.FileTransfer = health.SetupDefaultsFileTransfer{
		Enabled:      cfg.FileTransfer.Enabled,
		AllowedPaths: append([]string(nil), cfg.FileTransfer.AllowedPaths...),
		PasswordHash: cfg.FileTransfer.PasswordHash,
	}

	return resp
}

// reachableListenerAddress converts a listener bind address into something a
// remote peer could dial. For wildcard hosts (0.0.0.0, ::, empty), it
// substitutes the first non-loopback IPv4 interface address (falling back to
// IPv6). Port is preserved. Non-wildcard addresses are returned unchanged.
// Best-effort: returns the original address if we cannot improve on it.
func reachableListenerAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	if !isWildcardHost(host) {
		return address
	}
	if ip := firstRoutableInterfaceIP(); ip != "" {
		return net.JoinHostPort(ip, port)
	}
	return address
}

func isWildcardHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

// firstRoutableInterfaceIP returns the first non-loopback, non-link-local
// IPv4 address on any up interface, falling back to an IPv6 address if no
// IPv4 candidate exists.
func firstRoutableInterfaceIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var ipv6Fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				return ipv4.String()
			}
			if ipv6Fallback == "" && ip.To16() != nil {
				ipv6Fallback = ip.String()
			}
		}
	}
	return ipv6Fallback
}
