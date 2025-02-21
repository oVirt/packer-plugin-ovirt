package main

import (
	"fmt"
	"os"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
	"github.com/hashicorp/packer-plugin-sdk/version"
	"go.combell-sre.net/packer/builder-ovirt/builder/ovirt"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder("ovirt", new(ovirt.Builder))
	pps.SetVersion(version.InitializePluginVersion("v0.1.0", "dev"))
	err := pps.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
