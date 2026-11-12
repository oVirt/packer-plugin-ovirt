package ovirt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestBuilderRun_BootCommandVncEncryption(t *testing.T) {
	for _, tc := range []struct {
		name        string
		encryption  bool
		bootCommand []string
		wantVNCErr  bool
	}{
		{"encrypted with boot_command", true, []string{"<enter>"}, true},
		{"encrypted without boot_command", true, nil, false},
		{"unencrypted with boot_command", false, []string{"<enter>"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			createdVM := false
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/ovirt-engine/sso/oauth/token":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
				case r.URL.Path == "/ovirt-engine/api":
					w.Header().Set("Content-Type", "application/xml")
					_, _ = fmt.Fprint(w, `<api></api>`)
				case r.URL.Path == "/ovirt-engine/api/clusters":
					w.Header().Set("Content-Type", "application/xml")
					_, _ = fmt.Fprintf(w, `<clusters><cluster id="c1"><name>Default</name><vnc_encryption>%t</vnc_encryption></cluster></clusters>`, tc.encryption)
				case r.Method == http.MethodPost && r.URL.Path == "/ovirt-engine/api/vms":
					// Record the attempt and stop the build here.
					mu.Lock()
					createdVM = true
					mu.Unlock()
					w.WriteHeader(http.StatusBadRequest)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			raw := map[string]any{
				"ovirt_url":                srv.URL + "/ovirt-engine/api",
				"insecure_skip_tls_verify": true,
				"username":                 "admin@internal",
				"password":                 "password",
				"source_type":              "iso",
				"source_iso":               "test.iso",
				"communicator":             "none",
			}
			if tc.bootCommand != nil {
				raw["boot_command"] = tc.bootCommand
			}
			var b Builder
			if _, _, err := b.Prepare(raw); err != nil {
				t.Fatalf("Prepare: %s", err)
			}

			_, err := b.Run(context.Background(), packer.TestUi(t), &packer.MockHook{})
			if err == nil {
				t.Fatal("expected the build to fail (the fake engine rejects VM creation)")
			}
			gotVNCErr := strings.Contains(err.Error(), "VNC encryption")
			if gotVNCErr != tc.wantVNCErr {
				t.Fatalf("VNC encryption error = %v, want %v (err: %s)", gotVNCErr, tc.wantVNCErr, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if createdVM == tc.wantVNCErr {
				t.Fatalf("VM creation attempted = %v, want %v", createdVM, !tc.wantVNCErr)
			}
		})
	}
}
