// Package sysinfo collects system information for node info advertisements.
package sysinfo

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Muti-Metroo/Muti-Metroo/internal/protocol"
)

var (
	// Version is the agent version, set at build time via ldflags.
	// Example: go build -ldflags="-X github.com/Muti-Metroo/Muti-Metroo/internal/sysinfo.Version=1.0.0"
	Version = "dev"

	// startTime is when the agent started.
	startTime     time.Time
	startTimeOnce sync.Once

	// cachedShells holds the detected shells, computed once at init time.
	cachedShells []string
)

func init() {
	startTimeOnce.Do(func() {
		startTime = time.Now()
	})

	// Enhance "dev" version with VCS info from Go build system
	if Version == "dev" {
		Version = enhanceDevVersion()
	}

	// Detect available shells once at startup
	cachedShells = detectShells()
}

// enhanceDevVersion adds git commit info to dev version using Go's build info.
// Returns formats like: "dev-a1b2c3d", "dev-a1b2c3d-dirty", or "dev-<timestamp>" as fallback.
func enhanceDevVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		// Fallback to build timestamp if no build info available
		return "dev-" + startTime.UTC().Format("20060102-150405")
	}

	var revision string
	var dirty bool

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}

	if revision == "" {
		// No VCS info, use timestamp fallback
		return "dev-" + startTime.UTC().Format("20060102-150405")
	}

	// Shorten commit hash to 7 characters (standard git short hash)
	if len(revision) > 7 {
		revision = revision[:7]
	}

	if dirty {
		return "dev-" + revision + "-dirty"
	}
	return "dev-" + revision
}

// detectShells probes the system for known shells using exec.LookPath.
// Returns base names of found shells in preference order.
func detectShells() []string {
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{"powershell.exe", "pwsh.exe", "cmd.exe"}
	} else {
		candidates = []string{"bash", "sh", "zsh", "fish", "ash", "dash", "ksh"}
	}

	var found []string
	for _, shell := range candidates {
		if _, err := exec.LookPath(shell); err == nil {
			found = append(found, shell)
		}
	}
	return found
}

// DetectShells returns the list of available shells on the system.
// Results are cached at init time since shells don't change during runtime.
func DetectShells() []string {
	return cachedShells
}

// UDPConfig contains UDP relay configuration for node info advertisements.
type UDPConfig struct {
	Enabled bool
}

// ForwardConfig contains port forward listener configuration for node info advertisements.
type ForwardConfig struct {
	Listeners []protocol.ForwardListenerInfo
}

// FileTransferConfig contains file transfer configuration for node info advertisements.
type FileTransferConfig struct {
	Enabled bool
}

// ShellConfig contains shell configuration for node info advertisements.
type ShellConfig struct {
	Enabled bool
}

// ICMPConfig contains ICMP echo configuration for node info advertisements.
type ICMPConfig struct {
	Enabled bool
}

// Collect gathers local system information and returns a NodeInfo struct.
//
// The peers parameter contains current peer connection details to include in the advertisement.
// The publicKey parameter is the agent's X25519 public key for E2E encryption.
// Optional config parameters can be nil if the corresponding feature is not configured.
// advertisedListeners is the resolved list of public dial targets to surface in NodeInfo
// (nil/empty when no listener has an `advertise:` block configured).
func Collect(displayName string, peers []protocol.PeerConnectionInfo, publicKey [protocol.EphemeralKeySize]byte, udpConfig *UDPConfig, forwardConfig *ForwardConfig, fileTransferConfig *FileTransferConfig, shellConfig *ShellConfig, icmpConfig *ICMPConfig, advertisedListeners []protocol.AdvertisedListener) *protocol.NodeInfo {
	hostname, _ := os.Hostname()

	info := &protocol.NodeInfo{
		DisplayName:         displayName,
		Hostname:            hostname,
		OS:                  runtime.GOOS,
		Arch:                runtime.GOARCH,
		Version:             Version,
		StartTime:           startTime.Unix(),
		IPAddresses:         GetLocalIPs(),
		Peers:               peers,
		PublicKey:           publicKey,
		AdvertisedListeners: advertisedListeners,
		PID:                 os.Getpid(),
	}

	if udpConfig != nil {
		info.UDPEnabled = udpConfig.Enabled
	}

	if forwardConfig != nil {
		info.ForwardListeners = forwardConfig.Listeners
	}

	if fileTransferConfig != nil {
		info.FileTransferEnabled = fileTransferConfig.Enabled
	}

	if shellConfig != nil && shellConfig.Enabled {
		info.ShellEnabled = true
		info.Shells = cachedShells
	}

	if icmpConfig != nil {
		info.IcmpEnabled = icmpConfig.Enabled
	}

	return info
}

// GetLocalIPs returns non-loopback IPv4 addresses.
func GetLocalIPs() []string {
	var ips []string

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		// Skip loopback addresses
		if ipNet.IP.IsLoopback() {
			continue
		}

		// Only include IPv4 addresses (limit payload size)
		if ipv4 := ipNet.IP.To4(); ipv4 != nil {
			ips = append(ips, ipv4.String())
		}
	}

	// Limit to first 10 IPs to prevent payload bloat
	if len(ips) > 10 {
		ips = ips[:10]
	}

	return ips
}

// InterfaceAddr describes a single IP address assigned to a local network
// interface. It carries enough metadata for an operator to choose a source
// address for a route's outbound dial binding.
type InterfaceAddr struct {
	Interface string `json:"interface"`  // interface name (e.g. "eth0")
	IP        string `json:"ip"`         // address literal
	Family    string `json:"family"`     // "ipv4" or "ipv6"
	Loopback  bool   `json:"loopback"`   // address is a loopback address
	LinkLocal bool   `json:"link_local"` // address is link-local
	Global    bool   `json:"global"`     // routable: neither loopback nor link-local
}

// GetInterfaceAddresses returns every IP address assigned to an up interface,
// IPv4 and IPv6 alike, with no cap. Unlike GetLocalIPs (which is deliberately
// lossy to keep NodeInfo small), this is the complete picture used to populate
// a source-IP picker. Ordering is stable: interface order, then IPv4 before
// IPv6 within each interface, so a UI list does not jump between calls.
func GetInterfaceAddresses() []InterfaceAddr {
	var out []InterfaceAddr

	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var v4, v6 []InterfaceAddr
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			loopback := ip.IsLoopback()
			linkLocal := ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
			entry := InterfaceAddr{
				Interface: iface.Name,
				Loopback:  loopback,
				LinkLocal: linkLocal,
				Global:    !loopback && !linkLocal,
			}
			if v4ip := ip.To4(); v4ip != nil {
				entry.IP = v4ip.String()
				entry.Family = "ipv4"
				v4 = append(v4, entry)
			} else {
				entry.IP = ip.String()
				entry.Family = "ipv6"
				v6 = append(v6, entry)
			}
		}
		out = append(out, v4...)
		out = append(out, v6...)
	}

	return out
}

// InterfaceHasAddress reports whether ip is assigned to the named up interface.
// The second return value is false when the interface cannot be enumerated
// (down, absent, or the OS refused the query), letting callers distinguish "the
// address is definitely not there" from "we could not tell" -- the route
// validation path treats the latter as "defer to dial time" rather than a hard
// rejection. ip is compared family-aware so a v4-mapped form matches its v4
// address.
func InterfaceHasAddress(name string, ip net.IP) (has bool, resolved bool) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return false, false
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false, false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.Equal(ip) {
			return true, true
		}
	}
	return false, true
}

// PickInterfaceSource returns an address assigned to the named up interface to
// use as a dial source when the route selects an interface but no explicit
// source IP. It honors the requested family (wantV4) and prefers a global
// (routable) address over loopback/link-local. Returns nil when the interface
// has no usable address of that family, in which case the caller should fail
// the dial with a clear message.
func PickInterfaceSource(name string, wantV4 bool) net.IP {
	want := "ipv6"
	if wantV4 {
		want = "ipv4"
	}
	var fallback net.IP
	for _, a := range GetInterfaceAddresses() {
		if a.Interface != name || a.Family != want {
			continue
		}
		ip := net.ParseIP(a.IP)
		if ip == nil {
			continue
		}
		if a.Global {
			return ip
		}
		if fallback == nil {
			fallback = ip
		}
	}
	return fallback
}

// StartTime returns the agent start time.
func StartTime() time.Time {
	return startTime
}

// Uptime returns the agent uptime as a duration.
func Uptime() time.Duration {
	return time.Since(startTime)
}

// UptimeSeconds returns the agent uptime in seconds.
func UptimeSeconds() int64 {
	return int64(Uptime().Seconds())
}
