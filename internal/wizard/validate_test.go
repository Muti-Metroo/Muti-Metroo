package wizard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequired(t *testing.T) {
	v := Required("field")
	if err := v(""); err == nil {
		t.Error("empty string should fail Required")
	}
	if err := v("   "); err == nil {
		t.Error("whitespace-only string should fail Required")
	}
	if err := v("value"); err != nil {
		t.Errorf("non-empty string should pass Required: %v", err)
	}
}

func TestMaxLength(t *testing.T) {
	v := MaxLength(5)
	if err := v("12345"); err != nil {
		t.Errorf("exactly maxLen should pass: %v", err)
	}
	if err := v("123456"); err == nil {
		t.Error("over maxLen should fail")
	}
}

func TestIntRange(t *testing.T) {
	v := IntRange(1, 10, "count")
	for _, ok := range []string{"1", "5", "10"} {
		if err := v(ok); err != nil {
			t.Errorf("%s should pass IntRange(1,10): %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "11", "abc"} {
		if err := v(bad); err == nil {
			t.Errorf("%s should fail IntRange(1,10)", bad)
		}
	}
}

func TestYAMLExtension(t *testing.T) {
	v := YAMLExtension()
	for _, ok := range []string{"config.yaml", "a.yml", "/etc/muti-metroo/config.yaml"} {
		if err := v(ok); err != nil {
			t.Errorf("%s should pass YAMLExtension: %v", ok, err)
		}
	}
	for _, bad := range []string{"config.json", "config", "config.yam"} {
		if err := v(bad); err == nil {
			t.Errorf("%s should fail YAMLExtension", bad)
		}
	}
}

func TestIPv4CIDR(t *testing.T) {
	v := IPv4CIDR()
	if err := v("10.0.0.0/8"); err != nil {
		t.Errorf("valid IPv4 CIDR should pass: %v", err)
	}
	if err := v("fd00::/64"); err == nil {
		t.Error("IPv6 CIDR should fail IPv4CIDR")
	}
	if err := v("not-a-cidr"); err == nil {
		t.Error("garbage should fail IPv4CIDR")
	}
}

func TestIPv6CIDR(t *testing.T) {
	v := IPv6CIDR()
	if err := v("fd00::/64"); err != nil {
		t.Errorf("valid IPv6 CIDR should pass: %v", err)
	}
	if err := v("10.0.0.0/8"); err == nil {
		t.Error("IPv4 CIDR should fail IPv6CIDR")
	}
}

func TestCIDR(t *testing.T) {
	v := CIDR()
	for _, ok := range []string{"10.0.0.0/8", "fd00::/64", "0.0.0.0/0"} {
		if err := v(ok); err != nil {
			t.Errorf("%s should pass CIDR: %v", ok, err)
		}
	}
	if err := v("10.0.0.1"); err == nil {
		t.Error("bare IP without prefix should fail CIDR")
	}
}

func TestHostPort(t *testing.T) {
	v := HostPort()
	for _, ok := range []string{"127.0.0.1:1080", "example.com:443", "[::1]:8080"} {
		if err := v(ok); err != nil {
			t.Errorf("%s should pass HostPort: %v", ok, err)
		}
	}
	for _, bad := range []string{"noport", "host:", ":443"} {
		if err := v(bad); err == nil {
			t.Errorf("%s should fail HostPort", bad)
		}
	}
}

func TestHTTPOrHTTPSURL(t *testing.T) {
	v := HTTPOrHTTPSURL()
	for _, ok := range []string{"http://localhost:3000", "https://muti-metroo.example.com"} {
		if err := v(ok); err != nil {
			t.Errorf("%s should pass: %v", ok, err)
		}
	}
	for _, bad := range []string{"ftp://x", "localhost:3000", "://nohost"} {
		if err := v(bad); err == nil {
			t.Errorf("%s should fail HTTPOrHTTPSURL", bad)
		}
	}
}

func TestDuration(t *testing.T) {
	v := Duration()
	for _, ok := range []string{"30s", "5m", "1h30m"} {
		if err := v(ok); err != nil {
			t.Errorf("%s should pass Duration: %v", ok, err)
		}
	}
	if err := v("notaduration"); err == nil {
		t.Error("garbage should fail Duration")
	}
}

func TestChain(t *testing.T) {
	v := Chain(Required("field"), HostPort())
	if err := v(""); err == nil {
		t.Error("empty should fail the Required link")
	}
	if err := v("noport"); err == nil {
		t.Error("non-host:port should fail the HostPort link")
	}
	if err := v("host:1234"); err != nil {
		t.Errorf("valid value should pass the whole chain: %v", err)
	}
}

func TestParentDirExists(t *testing.T) {
	v := ParentDirExists()
	if err := v("config.yaml"); err != nil {
		t.Errorf("current-dir path should pass: %v", err)
	}

	dir := t.TempDir()
	if err := v(filepath.Join(dir, "config.yaml")); err != nil {
		t.Errorf("existing parent dir should pass: %v", err)
	}
	if err := v(filepath.Join(dir, "missing", "config.yaml")); err == nil {
		t.Error("missing parent dir should fail")
	}

	// A file (not a directory) as the parent should fail.
	f := filepath.Join(dir, "afile")
	if err := os.WriteFile(f, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := v(filepath.Join(f, "config.yaml")); err == nil {
		t.Error("file-as-parent should fail")
	}
}

func TestHTTPPath(t *testing.T) {
	v := HTTPPath()
	if err := v("/mesh"); err != nil {
		t.Errorf("/mesh should pass: %v", err)
	}
	for _, bad := range []string{"", "mesh", "mesh/"} {
		if err := v(bad); err == nil {
			t.Errorf("%q should fail HTTPPath", bad)
		}
	}
}

func TestHexKey(t *testing.T) {
	v := HexKey(64)
	valid := "aa11bb22cc33dd44ee55ff66778899aabbccddeeff00112233445566778899aa"
	if err := v(valid); err != nil {
		t.Errorf("64-char hex should pass: %v", err)
	}
	if err := v("0x" + valid); err != nil {
		t.Errorf("0x-prefixed 64-char hex should pass (normalized): %v", err)
	}
	if err := v("  " + valid + "  "); err != nil {
		t.Errorf("whitespace-padded hex should pass (normalized): %v", err)
	}
	if err := v("abcd"); err == nil {
		t.Error("wrong length should fail")
	}
	if err := v("zz11bb22cc33dd44ee55ff66778899aabbccddeeff00112233445566778899zz"); err == nil {
		t.Error("non-hex characters should fail")
	}
}
