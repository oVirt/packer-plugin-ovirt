package ovirt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

// createVMRequest runs stepCreateVM against a fake engine and returns the body
// of the VM creation request.
func createVMRequest(t *testing.T, extra map[string]any) string {
	t.Helper()
	var mu sync.Mutex
	var request string
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
		case path == "/vms" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			request = string(body)
			mu.Unlock()
			xml(`<vm id="vm1"><name>packer</name><status>down</status></vm>`)
		case path == "/vms/vm1":
			xml(`<vm id="vm1"><name>packer</name><status>down</status></vm>`)
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
		"source_template_id":       "11111111-1111-1111-1111-111111111111",
		"communicator":             "none",
	}
	for k, v := range extra {
		raw[k] = v
	}
	config, _, err := NewConfig(raw)
	if err != nil {
		t.Fatalf("NewConfig: %s", err)
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", packer.TestUi(t))
	state.Put("cluster_id", "c1")
	if action := (&stepCreateVM{}).Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v (error: %v)", action, state.Get("error"))
	}
	mu.Lock()
	defer mu.Unlock()
	return request
}

func TestStepCreateVM_TemplateKeepsSizing(t *testing.T) {
	request := createVMRequest(t, nil)
	for _, field := range []string{"<memory>", "<cpu>", "<tpm_enabled>"} {
		if strings.Contains(request, field) {
			t.Errorf("expected no %s in the request, so the template's value is kept: %s", field, request)
		}
	}
}

func TestStepCreateVM_TemplateExplicitSizing(t *testing.T) {
	request := createVMRequest(t, map[string]any{
		"cpu_cores": 4,
		"memory":    8192,
		"tpm":       true,
	})
	for _, want := range []string{"<cores>4</cores>", "<memory>8589934592</memory>", "<tpm_enabled>true</tpm_enabled>"} {
		if !strings.Contains(request, want) {
			t.Errorf("expected %s in the request: %s", want, request)
		}
	}
	if strings.Contains(request, "<sockets>") {
		t.Errorf("expected no <sockets>, it wasn't set: %s", request)
	}
}

func TestConfig_ISODefaultSizing(t *testing.T) {
	c, _, err := NewConfig(map[string]any{
		"ovirt_url":    "https://ovirt.example.com/ovirt-engine/api",
		"username":     "admin@internal",
		"password":     "password",
		"source_type":  "iso",
		"source_iso":   "test.iso",
		"communicator": "none",
	})
	if err != nil {
		t.Fatalf("NewConfig: %s", err)
	}
	if c.Cores != 1 || c.Sockets != 1 || c.Memory != 2048 {
		t.Fatalf("expected 1 core, 1 socket, 2048 MB for an ISO build, got %d, %d, %d", c.Cores, c.Sockets, c.Memory)
	}
	if c.Tpm != nil {
		t.Fatalf("expected tpm to be unset, got %v", *c.Tpm)
	}
}
