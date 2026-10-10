package forward

import (
	"testing"
)

func TestEndpoint(t *testing.T) {
	ep := Endpoint{
		Key:    "my-web-server",
		Target: "localhost:3000",
	}

	if ep.Key != "my-web-server" {
		t.Errorf("expected key 'my-web-server', got '%s'", ep.Key)
	}

	if ep.Target != "localhost:3000" {
		t.Errorf("expected target 'localhost:3000', got '%s'", ep.Target)
	}
}

func TestEndpoint_EmptyValues(t *testing.T) {
	ep := Endpoint{}

	if ep.Key != "" {
		t.Errorf("expected empty key, got '%s'", ep.Key)
	}

	if ep.Target != "" {
		t.Errorf("expected empty target, got '%s'", ep.Target)
	}
}
