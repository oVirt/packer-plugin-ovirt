package ovirt

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepDetachDisk struct{}

func (s *stepDetachDisk) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Say("Detaching disk from VM ...")

	resp, err := conn.SystemService().
		VmsService().
		VmService(vmID).
		DiskAttachmentsService().
		List().
		Send()
	if err != nil {
		err = fmt.Errorf("error listing disks of VM: %s", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	das := resp.MustAttachments()

	d, _ := conn.FollowLink(das.Slice()[0].MustDisk())
	disk, ok := d.(*ovirtsdk4.Disk)
	if !ok {
		err = fmt.Errorf("error getting disk of VM: '%s': %s", vmID, err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	diskID := disk.MustId()
	log.Printf("Disk identifier: %s", diskID)

	diskAttachmentService := conn.SystemService().
		VmsService().
		VmService(vmID).
		DiskAttachmentsService().
		AttachmentService(diskID)

	dasResp, err := diskAttachmentService.Get().Send()
	if err != nil {
		err = fmt.Errorf("error getting disk attachment '%s': %s", diskID, err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	if dasResp.MustAttachment().MustActive() {
		ui.Message(fmt.Sprintf("Deactivating disk attachment: %s ...", diskID))
		_, err := diskAttachmentService.Update().
			DiskAttachment(
				ovirtsdk4.NewDiskAttachmentBuilder().
					Active(false).
					MustBuild()).
			Send()
		if err != nil {
			err = fmt.Errorf("failed to deactivate disk attachment '%s': %s", diskID, err)
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}

	ui.Message("Waiting for disk attachment to become inactive ...")
	stateChange := StateChangeConf{
		Pending:   []string{"active"},
		Target:    []string{"inactive"},
		Refresh:   DiskAttachmentStateRefreshFunc(conn, vmID, diskID),
		StepState: state,
	}
	_, err = WaitForState(&stateChange)
	if err != nil {
		err := fmt.Errorf("failed waiting for disk attachment (%s) to become inactive: %s", diskID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	_, err = diskAttachmentService.Remove().Send()
	if err != nil {
		err := fmt.Errorf("failed to detach disk (%s) from VM: %s", diskID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	state.Put("disk_id", diskID)

	return multistep.ActionContinue
}

func (s *stepDetachDisk) Cleanup(state multistep.StateBag) {}
