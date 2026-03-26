package ovirt

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepUpdateDisk struct{}

func (s *stepUpdateDisk) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Say("Updating disk properties ...")

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

	_, err = diskAttachmentService.Get().Send()
	if err != nil {
		err = fmt.Errorf("error getting disk attachment '%s': %s", diskID, err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	diskBuilder := ovirtsdk4.NewDiskBuilder()
	diskBuilder.Description(config.DiskDescription)

	if len(config.DiskName) != 0 {
		diskBuilder.Name(config.DiskName)
	} else if len(config.TemplateName) != 0 {
		diskBuilder.Name(config.TemplateName)
	}
	disk = diskBuilder.MustBuild()

	log.Printf("Disk name: %s", disk.MustName())
	log.Printf("Disk description: %s", config.DiskDescription)

	_, err = diskAttachmentService.Update().DiskAttachment(
		ovirtsdk4.NewDiskAttachmentBuilder().
			Disk(disk).
			MustBuild()).
		Send()
	if err != nil {
		err = fmt.Errorf("failed to update disk properties: %s", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("Waiting for disk '%s' reaching status OK...", diskID))
	stateChange := StateChangeConf{
		Pending:   []string{string(ovirtsdk4.DISKSTATUS_LOCKED)},
		Target:    []string{string(ovirtsdk4.DISKSTATUS_OK)},
		Refresh:   DiskStateRefreshFunc(conn, diskID),
		StepState: state,
	}
	_, err = WaitForState(&stateChange)
	if err != nil {
		err := fmt.Errorf("failed waiting for disk attachment (%s) to become inactive: %s", diskID, err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepUpdateDisk) Cleanup(state multistep.StateBag) {}
