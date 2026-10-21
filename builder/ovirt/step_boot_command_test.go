package ovirt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

// TestStepBootCommand_ErrorIsError checks that a failing boot command step
// stores an error in the state bag. Builder.Run asserts the stored value to
// error, so a string there made the build panic and exit 0.
func TestStepBootCommand_ErrorIsError(t *testing.T) {
	// An engine whose VM has no display, so the step fails early.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ovirt-engine/sso/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
		case "/ovirt-engine/api":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, `<api></api>`)
		case "/ovirt-engine/api/vms/vm1":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, `<vm id="vm1"><name>packer</name><status>up</status></vm>`)
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
	config.BootCommand.VNCConfig.BootCommand = []string{"<enter>"}

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", packer.TestUi(t))
	state.Put("vm_id", "vm1")

	step := &stepBootCommand{}
	if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}

	raw, ok := state.GetOk("error")
	if !ok {
		t.Fatal("expected an error in the state bag")
	}
	if _, ok := raw.(error); !ok {
		t.Fatalf("expected the state bag error to be an error, got %T: %v", raw, raw)
	}
}
