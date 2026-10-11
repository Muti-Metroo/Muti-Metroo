package wizard

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// orDefault returns existing when it is non-empty, else fallback. It seeds a
// prompt default from a loaded config without clobbering the wizard's built-in
// default when the field was never set. Mirrors the helper of the same name in
// Mutiauk's wizard.
func orDefault(existing, fallback string) string {
	if existing != "" {
		return existing
	}
	return fallback
}

// Validator validates a string input and returns an error if it is invalid.
//
// This mirrors the composable validator library in Mutiauk
// (internal/wizard/validate.go there) so the two setup wizards validate input
// the same way and read the same way to develop. Use them with
// prompt.ReadLineValidated and combine them with Chain.
type Validator func(string) error

// Required returns a validator that ensures the value is not empty.
func Required(fieldName string) Validator {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", fieldName)
		}
		return nil
	}
}

// MaxLength returns a validator that ensures the value does not exceed maxLen characters.
func MaxLength(maxLen int) Validator {
	return func(s string) error {
		if len(s) > maxLen {
			return fmt.Errorf("must be %d characters or less", maxLen)
		}
		return nil
	}
}

// IntRange returns a validator that parses an integer and ensures it is within
// the given inclusive range.
func IntRange(min, max int, fieldName string) Validator {
	return func(s string) error {
		var val int
		if _, err := fmt.Sscanf(s, "%d", &val); err != nil {
			return fmt.Errorf("invalid %s: must be a number", fieldName)
		}
		if val < min || val > max {
			return fmt.Errorf("%s must be between %d and %d", fieldName, min, max)
		}
		return nil
	}
}

// YAMLExtension validates that the path ends with a .yaml or .yml extension.
func YAMLExtension() Validator {
	return func(s string) error {
		if !strings.HasSuffix(s, ".yaml") && !strings.HasSuffix(s, ".yml") {
			return fmt.Errorf("config file should have .yaml or .yml extension")
		}
		return nil
	}
}

// IPv4CIDR validates that the string is a valid IPv4 CIDR notation.
func IPv4CIDR() Validator {
	return func(s string) error {
		ip, _, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR format: %v", err)
		}
		if ip.To4() == nil {
			return fmt.Errorf("must be an IPv4 address")
		}
		return nil
	}
}

// IPv6CIDR validates that the string is a valid IPv6 CIDR notation.
func IPv6CIDR() Validator {
	return func(s string) error {
		ip, _, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR format: %v", err)
		}
		if ip.To4() != nil {
			return fmt.Errorf("must be an IPv6 address")
		}
		return nil
	}
}

// CIDR validates that the string is a valid CIDR notation (IPv4 or IPv6).
func CIDR() Validator {
	return func(s string) error {
		if _, _, err := net.ParseCIDR(s); err != nil {
			return fmt.Errorf("invalid CIDR format: %v", err)
		}
		return nil
	}
}

// HostPort validates that the string is a valid host:port address.
func HostPort() Validator {
	return func(s string) error {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			return fmt.Errorf("invalid address format (use host:port)")
		}
		if host == "" || port == "" {
			return fmt.Errorf("both host and port are required")
		}
		return nil
	}
}

// HTTPOrHTTPSURL validates that the string is a valid HTTP or HTTPS URL.
func HTTPOrHTTPSURL() Validator {
	return func(s string) error {
		u, err := url.Parse(s)
		if err != nil {
			return fmt.Errorf("invalid URL: %v", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("URL must use http or https scheme")
		}
		if u.Host == "" {
			return fmt.Errorf("URL must include host")
		}
		return nil
	}
}

// Duration validates that the string is a valid Go duration format.
func Duration() Validator {
	return func(s string) error {
		if _, err := time.ParseDuration(s); err != nil {
			return fmt.Errorf("invalid duration (use format like 30s, 1m, 5m)")
		}
		return nil
	}
}

// Chain combines multiple validators into one. All validators must pass, in order.
func Chain(validators ...Validator) Validator {
	return func(s string) error {
		for _, v := range validators {
			if err := v(s); err != nil {
				return err
			}
		}
		return nil
	}
}

// --- Muti Metroo-specific validators (no Mutiauk counterpart) ---

// ParentDirExists validates that the parent directory of the given path exists.
// The current directory (".") is always considered present.
func ParentDirExists() Validator {
	return func(s string) error {
		parent := filepath.Dir(s)
		if parent == "." {
			return nil
		}
		if info, err := os.Stat(parent); err != nil || !info.IsDir() {
			return fmt.Errorf("parent directory does not exist: %s", parent)
		}
		return nil
	}
}

// HTTPPath validates that the value is a non-empty path beginning with "/".
func HTTPPath() Validator {
	return func(s string) error {
		if s == "" || !strings.HasPrefix(s, "/") {
			return fmt.Errorf("path must start with /")
		}
		return nil
	}
}

// HexKey validates that the value (after stripping an optional 0x prefix and
// surrounding whitespace, matching normalizeHexKey) is exactly chars hex
// digits long. Used for management/signing public keys entered by hand.
func HexKey(chars int) Validator {
	return func(s string) error {
		s = normalizeHexKey(s)
		if len(s) != chars {
			return fmt.Errorf("key must be %d hex characters (got %d)", chars, len(s))
		}
		if _, err := hex.DecodeString(s); err != nil {
			return fmt.Errorf("invalid hex string: %v", err)
		}
		return nil
	}
}
