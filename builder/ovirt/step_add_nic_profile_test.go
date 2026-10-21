package ovirt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

// TestStepAddNic_UnknownVnicProfile checks that a vnic_profile name that
// doesn't exist on the network is an error, not a silent fallback to the
// network's first profile.
func TestStepAddNic_UnknownVnicProfile(t *testing.T) {
	var mu sync.Mutex
	nicAdded := false
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
		case path == "/networks", path == "/clusters/c1/networks":
			xml(`<networks><network id="n1"><name>lab</name></network></networks>`)
		case path == "/networks/n1/vnicprofiles":
			xml(`<vnic_profiles><vnic_profile id="p1"><name>lab</name></vnic_profile>` +
				`<vnic_profile id="p2"><name>lab-mirror</name></vnic_profile></vnic_profiles>`)
		case path == "/vms/vm1/nics" && r.Method == http.MethodGet:
			xml(`<nics></nics>`)
		case path == "/vms/vm1/nics" && r.Method == http.MethodPost:
			mu.Lock()
			nicAdded = true
			mu.Unlock()
			xml(`<nic id="nic1"><name>nic1</name></nic>`)
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
	config.Network = "lab"
	config.VNICProfile = "lab-mirorr"

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", packer.TestUi(t))
	state.Put("vm_id", "vm1")
	state.Put("cluster_id", "c1")

	if action := (&stepAddNic{}).Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}
	raw, _ := state.GetOk("error")
	if err, ok := raw.(error); !ok || !strings.Contains(err.Error(), "could not find VNIC profile 'lab-mirorr'") {
		t.Fatalf("unexpected error: %v", raw)
	}
	mu.Lock()
	defer mu.Unlock()
	if nicAdded {
		t.Fatal("expected no NIC to be added")
	}
}
