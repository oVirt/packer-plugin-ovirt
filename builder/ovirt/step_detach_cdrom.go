package ovirt

import (
	"context"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepDetachCDrom struct{}

func (s *stepDetachCDrom) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	vmService := conn.SystemService().VmsService().VmService(vmID)

	ui.Say("Detaching CD-ROM from VM ...")

	cdromService := vmService.CdromsService()
	resp, err := cdromService.List().Send()
	if err != nil {
		err = fmt.Errorf("could not list CDROM: %s", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	cdroms := resp.MustCdroms().Slice()

	// To eject the disk use a file with an empty id:
	// https://ovirt.github.io/ovirt-engine-api-model/master/#services/vm_cdrom/methods/update
	empty := ovirtsdk4.NewCdromBuilder().FileBuilder(ovirtsdk4.NewFileBuilder().Id("")).MustBuild()

	for i := range cdroms {
		if _, err := cdromService.CdromService(cdroms[i].MustId()).Update().Cdrom(empty).Send(); err != nil {
			err = fmt.Errorf("could not detach CDROM: %s", err)
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}

	return multistep.ActionContinue
}

func (s *stepDetachCDrom) Cleanup(state multistep.StateBag) {}
