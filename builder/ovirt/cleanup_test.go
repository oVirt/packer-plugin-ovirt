package ovirt

import (
	"net/url"
	"testing"

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
