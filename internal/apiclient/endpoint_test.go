package apiclient

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Endpoint
		wantErr bool
	}{
		{name: "bare host port", in: "localhost:8080", want: Endpoint{Scheme: "http", Host: "localhost:8080"}},
		{name: "bare host port with whitespace", in: "  localhost:8080  ", want: Endpoint{Scheme: "http", Host: "localhost:8080"}},
		{name: "explicit http", in: "http://localhost:8080", want: Endpoint{Scheme: "http", Host: "localhost:8080"}},
		{name: "http with prefix", in: "http://host:8080/prefix", want: Endpoint{Scheme: "http", Host: "host:8080", PathPrefix: "/prefix"}},
		{name: "http prefix trailing slash trimmed", in: "http://host:8080/prefix/", want: Endpoint{Scheme: "http", Host: "host:8080", PathPrefix: "/prefix"}},
		{name: "https no port", in: "https://example.com", want: Endpoint{Scheme: "https", Host: "example.com"}},
		{name: "https with port and prefix", in: "https://example.com:8443/muti-metroo", want: Endpoint{Scheme: "https", Host: "example.com:8443", PathPrefix: "/muti-metroo"}},
		{name: "https root slash only", in: "https://example.com/", want: Endpoint{Scheme: "https", Host: "example.com"}},

		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "unsupported scheme", in: "ftp://host", wantErr: true},
		{name: "scheme without host", in: "http://", wantErr: true},
		{name: "malformed scheme", in: "://host", wantErr: true},
		{name: "host with path no scheme", in: "host:8080/prefix", wantErr: true},
		{name: "credentials", in: "http://user:pw@host:8080", wantErr: true},
		{name: "query", in: "http://host:8080/?foo=bar", wantErr: true},
		{name: "fragment", in: "http://host:8080/#frag", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestHTTPURL(t *testing.T) {
	tests := []struct {
		ep   Endpoint
		path string
		want string
	}{
		{Endpoint{Scheme: "http", Host: "localhost:8080"}, "/healthz", "http://localhost:8080/healthz"},
		{Endpoint{Scheme: "http", Host: "localhost:8080"}, "healthz", "http://localhost:8080/healthz"},
		{Endpoint{Scheme: "http", Host: "localhost:8080"}, "", "http://localhost:8080/"},
		{Endpoint{Scheme: "https", Host: "example.com", PathPrefix: "/muti-metroo"}, "/agents", "https://example.com/muti-metroo/agents"},
		{Endpoint{Scheme: "https", Host: "example.com:8443", PathPrefix: "/muti-metroo"}, "/agents/abc/file/upload", "https://example.com:8443/muti-metroo/agents/abc/file/upload"},
	}

	for _, tc := range tests {
		got := tc.ep.HTTPURL(tc.path)
		if got != tc.want {
			t.Errorf("HTTPURL(%+v, %q) = %q, want %q", tc.ep, tc.path, got, tc.want)
		}
	}
}

func TestWSURL(t *testing.T) {
	tests := []struct {
		ep   Endpoint
		path string
		want string
	}{
		{Endpoint{Scheme: "http", Host: "localhost:8080"}, "/agents/abc/shell", "ws://localhost:8080/agents/abc/shell"},
		{Endpoint{Scheme: "https", Host: "example.com"}, "/agents/abc/shell", "wss://example.com/agents/abc/shell"},
		{Endpoint{Scheme: "https", Host: "example.com", PathPrefix: "/muti-metroo"}, "/agents/abc/icmp", "wss://example.com/muti-metroo/agents/abc/icmp"},
		{Endpoint{Scheme: "http", Host: "localhost:8080"}, "agents/abc/shell?mode=tty", "ws://localhost:8080/agents/abc/shell?mode=tty"},
	}

	for _, tc := range tests {
		got := tc.ep.WSURL(tc.path)
		if got != tc.want {
			t.Errorf("WSURL(%+v, %q) = %q, want %q", tc.ep, tc.path, got, tc.want)
		}
	}
}
