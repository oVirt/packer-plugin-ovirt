package ovirt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/packer"
)

// newFakeEngine starts a TLS server that answers the oVirt SSO login and the
// given "METHOD /path" routes (paths relative to /ovirt-engine/api) with XML.
// Everything else gets a 404.
func newFakeEngine(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ovirt-engine/sso/oauth/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"access_token":"token"}`)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/ovirt-engine/api")
		if body, ok := routes[r.Method+" "+path]; ok {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = fmt.Fprint(w, body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBuilderRun_PanicFailsBuild(t *testing.T) {
	// The engine answers the VM creation without an id, so MustId() panics.
	srv := newFakeEngine(t, map[string]string{
		"GET ":          `<api></api>`,
		"GET /clusters": `<clusters><cluster id="c1"><name>Default</name></cluster></clusters>`,
		"POST /vms":     `<vm><name>packer</name></vm>`,
	})

	var b Builder
	if _, _, err := b.Prepare(map[string]any{
		"ovirt_url":                srv.URL + "/ovirt-engine/api",
		"insecure_skip_tls_verify": true,
		"username":                 "admin@internal",
		"password":                 "password",
		"source_template_id":       "11111111-1111-1111-1111-111111111111",
		"communicator":             "none",
	}); err != nil {
		t.Fatalf("Prepare: %s", err)
	}

	artifact, err := b.Run(context.Background(), packer.TestUi(t), &packer.MockHook{})
	if err == nil {
		t.Fatalf("expected an error after a panic, got artifact=%v err=nil", artifact)
	}
	if artifact != nil {
		t.Fatalf("expected no artifact, got %v", artifact)
	}
	if !strings.Contains(err.Error(), "unexpected panic") {
		t.Fatalf("unexpected error: %s", err)
	}
}
