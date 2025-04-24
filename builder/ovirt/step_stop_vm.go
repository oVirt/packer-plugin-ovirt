package ovirt

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepStopVM struct{}

func (s *stepStopVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	conn := state.Get("conn").(*ovirtsdk4.Connection)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	ui.Say(fmt.Sprintf("Stopping VM: %s ...", vmID))
	if len(config.ShutdownCommand) > 0 {
		ui.Message(fmt.Sprintf("Sending shutdown command to VM: %s ...", vmID))

		comm, _ := state.Get("communicator").(packer.Communicator)

		var stdout, stderr bytes.Buffer
		cmd := &packer.RemoteCmd{
			Command: config.ShutdownCommand,
			Stdout:  &stdout,
			Stderr:  &stderr,
		}
		err := comm.Start(ctx, cmd)
		if err != nil {
			state.Put("error", fmt.Errorf("error sending shutdown command: %s", err))
			return multistep.ActionHalt
		}

	} else {
		_, err := conn.SystemService().
			VmsService().
			VmService(vmID).
			Shutdown().
			Send()
		if err != nil {
			err = fmt.Errorf("Error shutting down VM: %s", err)
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}

	ui.Message(fmt.Sprintf("Waiting for VM to stop: %s ...", vmID))
	stateChange := StateChangeConf{
		Pending:   []string{string(ovirtsdk4.VMSTATUS_UP), string(ovirtsdk4.VMSTATUS_POWERING_DOWN)},
		Target:    []string{string(ovirtsdk4.VMSTATUS_DOWN)},
		Refresh:   VMStateRefreshFunc(conn, vmID),
		StepState: state,
	}
	if _, err := WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("Error waiting for VM (%s) to stop: %s", vmID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepStopVM) Cleanup(state multistep.StateBag) {}
