package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/service"
)

func TestNewServiceStatusInfo(t *testing.T) {
	t.Run("system service", func(t *testing.T) {
		got := newServiceStatusInfo(true, "system", "muti-metroo", "active", "", nil)
		if !got.Installed || got.Scope != "system" || got.ServiceName != "muti-metroo" || got.Status != "active" {
			t.Errorf("unexpected system info: %+v", got)
		}
		if got.Platform != service.Platform() {
			t.Errorf("Platform = %q, want %q", got.Platform, service.Platform())
		}
		if got.ConfigPath != "" || got.LogPath != "" || got.DLLPath != "" {
			t.Errorf("system service should carry no paths: %+v", got)
		}
	})

	t.Run("user service copies paths", func(t *testing.T) {
		info := &service.UserServiceInfo{ConfigPath: "/etc/muti-metroo/config.yaml", LogPath: "/home/u/.muti-metroo/muti-metroo.log", DLLPath: "C:/muti-metroo.dll"}
		got := newServiceStatusInfo(true, "user", "muti-metroo", "active (pid 42, cron+nohup)", "cron+nohup", info)
		if got.Scope != "user" || got.Type != "cron+nohup" {
			t.Errorf("unexpected user info: %+v", got)
		}
		if got.ConfigPath != info.ConfigPath || got.LogPath != info.LogPath || got.DLLPath != info.DLLPath {
			t.Errorf("paths not copied from UserServiceInfo: %+v", got)
		}
	})

	t.Run("not installed omits optional fields in JSON", func(t *testing.T) {
		got := newServiceStatusInfo(false, "none", "muti-metroo", "", "", nil)
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		// Always-present contract fields.
		for _, key := range []string{`"installed":false`, `"scope":"none"`, `"service_name":"muti-metroo"`, `"platform":`} {
			if !strings.Contains(s, key) {
				t.Errorf("JSON missing %s: %s", key, s)
			}
		}
		// omitempty fields must be absent when empty.
		for _, key := range []string{"status", "type", "config_path", "log_path", "dll_path"} {
			if strings.Contains(s, `"`+key+`"`) {
				t.Errorf("empty %q should be omitted: %s", key, s)
			}
		}
	})
}

func TestBreakawayServiceHint(t *testing.T) {
	admin := breakawayServiceHint(true)
	if !strings.Contains(admin, "muti-metroo service install -c") {
		t.Errorf("admin hint should suggest system install: %q", admin)
	}
	if strings.Contains(admin, "--user") {
		t.Errorf("admin hint should not mention --user: %q", admin)
	}

	nonAdmin := breakawayServiceHint(false)
	if !strings.Contains(nonAdmin, "--user --dll") {
		t.Errorf("non-admin hint should suggest --user --dll install: %q", nonAdmin)
	}
	if !strings.Contains(nonAdmin, "Administrator") {
		t.Errorf("non-admin hint should explain the privilege reason: %q", nonAdmin)
	}
}
