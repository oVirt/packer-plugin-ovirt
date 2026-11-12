package ovirt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

// TestDiskSteps_NoDisks checks that the disk steps fail with an error, instead
// of indexing an empty list, when the VM has no disk attachments.
func TestDiskSteps_NoDisks(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ovirt-engine/sso/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
		case "/ovirt-engine/api":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, `<api></api>`)
		case "/ovirt-engine/api/vms/vm1/diskattachments":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, `<disk_attachments></disk_attachments>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL + "/ovirt-engine/api")
	if err != nil {
		t.Fatal(err)
	}

	for name, step := range map[string]multistep.Step{
		"update": &stepUpdateDisk{},
		"detach": &stepDetachDisk{},
	} {
		t.Run(name, func(t *testing.T) {
			config := &Config{}
			config.ovirtURL = u
			config.Username = "admin@internal"
			config.Password = "password"
			config.SkipCertValidation = true

			state := new(multistep.BasicStateBag)
			state.Put("config", config)
			state.Put("ui", packer.TestUi(t))
			state.Put("vm_id", "vm1")

			if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
				t.Fatalf("expected ActionHalt, got %v", action)
			}
			raw, ok := state.GetOk("error")
			if !ok {
				t.Fatal("expected an error in the state bag")
			}
			if err, ok := raw.(error); !ok || !strings.Contains(err.Error(), "has no disks") {
				t.Fatalf("unexpected error: %v", raw)
			}
		})
	}
}
