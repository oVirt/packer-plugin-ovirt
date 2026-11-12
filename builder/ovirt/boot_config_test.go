package ovirt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func isoBootConfig(extra map[string]any) map[string]any {
	raw := map[string]any{
		"ovirt_url":    "https://ovirt.example.com/ovirt-engine/api",
		"username":     "admin@internal",
		"password":     "password",
		"source_type":  "iso",
		"source_iso":   "test.iso",
		"boot_command": []string{"<enter>"},
		"communicator": "none",
	}
	for k, v := range extra {
		raw[k] = v
	}
	return raw
}

func TestConfig_BootWaitDefault(t *testing.T) {
	c, _, err := NewConfig(isoBootConfig(nil))
	if err != nil {
		t.Fatalf("NewConfig: %s", err)
	}
	if c.BootCommand.VNCConfig.BootWait != 10*time.Second {
		t.Fatalf("expected the SDK's default boot_wait of 10s, got %s", c.BootCommand.VNCConfig.BootWait)
	}
}

func TestConfig_BootCommandValidated(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"negative wait":            {"boot_command": []string{"<wait-5s><enter>"}},
		"boot command without vnc": {"disable_vnc": true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := NewConfig(isoBootConfig(extra)); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

// TestStepSetupInitialRun_BootCommandVMAlreadyUp checks that, when a boot
// command will be typed, a VM that is already up at the first poll is fine.
// Waiting for powering_up only made that an "unexpected state 'up'" error.
func TestStepSetupInitialRun_BootCommandVMAlreadyUp(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		xml := func(body string) {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, body)
		}
		switch path := strings.TrimPrefix(r.URL.Path, "/ovirt-engine/api"); {
		case r.URL.Path == "/ovirt-engine/sso/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
		case path == "":
			xml(`<api></api>`)
		case path == "/disks":
			xml(`<disks><disk id="iso1"><name>test.iso</name></disk></disks>`)
		case path == "/vms/vm1/cdroms" && r.Method == http.MethodPost:
			xml(`<cdrom id="cd1"></cdrom>`)
		case path == "/vms/vm1/start" && r.Method == http.MethodPost:
			xml(`<action><status>complete</status></action>`)
		case path == "/vms/vm1":
			xml(`<vm id="vm1"><name>packer</name><status>up</status></vm>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	config, _, err := NewConfig(isoBootConfig(map[string]any{
		"ovirt_url":                srv.URL + "/ovirt-engine/api",
		"insecure_skip_tls_verify": true,
		"boot_wait":                "5s",
	}))
	if err != nil {
		t.Fatalf("NewConfig: %s", err)
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", packer.TestUi(t))
	state.Put("vm_id", "vm1")

	step := &stepSetupInitialRun{Comm: &config.Comm}
	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v (error: %v)", action, state.Get("error"))
	}
}
