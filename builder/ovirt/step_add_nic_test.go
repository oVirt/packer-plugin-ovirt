package ovirt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

// TestStepAddNic_NetworkFromCluster checks that the network is looked up among
// the build cluster's networks. The engine has an "ovirtmgmt" network in two
// data centers: n1 (another data center, what a global search returns first)
// and n2 (attached to the build cluster c2).
func TestStepAddNic_NetworkFromCluster(t *testing.T) {
	var mu sync.Mutex
	var addedNic string
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
		case path == "/networks":
			xml(`<networks><network id="n1"><name>ovirtmgmt</name></network></networks>`)
		case path == "/clusters/c2/networks":
			xml(`<networks><network id="n2"><name>ovirtmgmt</name></network></networks>`)
		case path == "/networks/n1/vnicprofiles":
			xml(`<vnic_profiles><vnic_profile id="p1"><name>ovirtmgmt</name></vnic_profile></vnic_profiles>`)
		case path == "/networks/n2/vnicprofiles":
			xml(`<vnic_profiles><vnic_profile id="p2"><name>ovirtmgmt</name></vnic_profile></vnic_profiles>`)
		case path == "/vms/vm1/nics" && r.Method == http.MethodGet:
			xml(`<nics></nics>`)
		case path == "/vms/vm1/nics" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			addedNic = string(body)
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
	config.Network = "ovirtmgmt"
	config.VNICProfile = "ovirtmgmt"

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", packer.TestUi(t))
	state.Put("vm_id", "vm1")
	state.Put("cluster_id", "c2")

	if action := (&stepAddNic{}).Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v (error: %v)", action, state.Get("error"))
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(addedNic, `<vnic_profile id="p2">`) {
		t.Fatalf("expected the NIC on the build cluster's profile p2, got: %s", addedNic)
	}
}
