package main

import (
	"go.combell-sre.net/packer-builder-ovirt/builder/ovirt"
	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

func main() {
	server, err := plugin.Server()
	if err != nil {
		panic(err)
	}
	server.RegisterBuilder(new(ovirt.Builder))
	server.Serve()
}
