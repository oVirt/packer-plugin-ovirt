package ovirt

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestConfig_DiskNameDefault(t *testing.T) {
	raw := map[string]any{
		"ovirt_url":            "https://ovirt.example.com/ovirt-engine/api",
		"username":             "admin@internal",
		"password":             "password",
		"source_template_name": "template",
		"communicator":         "none",
		"vm_name":              "build-vm",
	}

	c, _, err := NewConfig(raw)
	if err != nil {
		t.Fatalf("NewConfig: %s", err)
	}
	if c.DiskName != "build-vm" {
		t.Fatalf("expected disk_name to default to vm_name, got %q", c.DiskName)
	}

	raw["template_name"] = "golden"
	c, _, err = NewConfig(raw)
	if err != nil {
		t.Fatalf("NewConfig: %s", err)
	}
	if c.DiskName != "golden" {
		t.Fatalf("expected disk_name to default to template_name, got %q", c.DiskName)
	}
}

// TestCleanup_ReconnectFails checks that the cleanups don't use a nil
// connection when ovirtConnect fails.
func TestCleanup_ReconnectFails(t *testing.T) {
	u, err := url.Parse("https://ovirt.example.com/ovirt-engine/api")
	if err != nil {
		t.Fatal(err)
	}

	for name, step := range map[string]multistep.Step{
		"create vm":   &stepCreateVM{},
		"initial run": &stepSetupInitialRun{},
	} {
		t.Run(name, func(t *testing.T) {
			// No connection in the state bag and no username, so
			// ovirtConnect fails without contacting anything.
			config := &Config{}
			config.ovirtURL = u

			state := new(multistep.BasicStateBag)
			state.Put("config", config)
			state.Put("ui", packer.TestUi(t))
			state.Put("vm_id", "vm1")

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Cleanup panicked: %v", r)
				}
			}()
			step.Cleanup(state)
		})
	}
}

// TestStepSetupInitialRunCleanup_ShutdownTimeout checks that the cleanup
// stops waiting after shutdown_timeout when the VM never goes down.
func TestStepSetupInitialRunCleanup_ShutdownTimeout(t *testing.T) {
	fastPoll(t)
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
		case path == "/vms/vm1/stop" && r.Method == http.MethodPost:
			xml(`<action></action>`)
		case path == "/vms/vm1" && r.Method == http.MethodGet:
			xml(`<vm id="vm1"><status>powering_down</status></vm>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL + "/ovirt-engine/api")
	if err != nil {
		t.Fatal(err)
	}
	config := &Config{}
	config.ovirtURL = u
	config.Username = "admin@internal"
	config.Password = "password"
	config.SkipCertValidation = true
	config.ShutdownTimeout = 50 * time.Millisecond

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", packer.TestUi(t))
	state.Put("vm_id", "vm1")

	done := make(chan struct{})
	go func() {
		defer close(done)
		(&stepSetupInitialRun{}).Cleanup(state)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Cleanup kept waiting past shutdown_timeout")
	}
}
