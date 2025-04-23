package ovirt

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepAddDisk struct{}

func (s *stepAddDisk) findStorageDomain(conn *ovirtsdk4.Connection, name string) (string, error) {
	log.Printf("Searching for storage domain '%s'", name)
	service := conn.SystemService().StorageDomainsService()
	resp, err := service.List().Search(fmt.Sprintf("name=%s", name)).Max(1).Send()
	if err != nil {
		return "", fmt.Errorf("error while searching for storage domain '%s': %s", name, err)
	}
	domains := resp.MustStorageDomains().Slice()

	if len(domains) == 0 {
		return "", fmt.Errorf("could not find storage domain '%s'", name)
	}
	return domains[0].MustId(), nil
}

func (s *stepAddDisk) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	conn := state.Get("conn").(*ovirtsdk4.Connection)
	vmID := state.Get("vm_id").(string)

	storageDomainID, err := s.findStorageDomain(conn, config.StorageDomain)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	domain := ovirtsdk4.NewStorageDomainBuilder().Id(storageDomainID).MustBuild()

	domains := new(ovirtsdk4.StorageDomainSlice)
	domains.SetSlice([]*ovirtsdk4.StorageDomain{domain})

	size := int64(config.DiskSize * 1024 * 1024 * 1024) // TODO: config

	diskBuilder := ovirtsdk4.NewDiskBuilder()
	diskBuilder.Name(fmt.Sprintf("%s_Disk1", config.VMName))
	diskBuilder.TotalSize(size)
	diskBuilder.ProvisionedSize(size)
	diskBuilder.Bootable(true)
	diskBuilder.Format(ovirtsdk4.DISKFORMAT_COW)
	diskBuilder.StorageDomain(domain)
	diskBuilder.StorageDomains(domains)
	diskBuilder.StorageType(ovirtsdk4.DISKSTORAGETYPE_IMAGE)

	diskResp, err := conn.SystemService().DisksService().Add().Disk(diskBuilder.MustBuild()).Send()
	if err != nil {
		err = fmt.Errorf("could not create disk: %w", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	disk := diskResp.MustDisk()
	diskID := disk.MustId()
	state.Put("disk_id", diskID)

	ui.Message(fmt.Sprintf("Waiting for disk '%s' reaching status OK...", diskID))
	stateChange := StateChangeConf{
		Pending:   []string{string(ovirtsdk4.DISKSTATUS_LOCKED)},
		Target:    []string{string(ovirtsdk4.DISKSTATUS_OK)},
		Refresh:   DiskStateRefreshFunc(conn, diskID),
		StepState: state,
	}
	if _, err = WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("failed waiting for disk (%s) to reach status OK: %w", diskID, err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	attachmentBuilder := ovirtsdk4.NewDiskAttachmentBuilder()
	attachmentBuilder.Bootable(true)
	attachmentBuilder.Active(true)
	attachmentBuilder.Interface(ovirtsdk4.DISKINTERFACE_VIRTIO_SCSI)
	attachmentBuilder.Disk(disk)

	dasService := conn.SystemService().VmsService().VmService(vmID).DiskAttachmentsService()
	if _, err := dasService.Add().Attachment(attachmentBuilder.MustBuild()).Send(); err != nil {
		err = fmt.Errorf("could not attach disk: %s", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Message(fmt.Sprintf("Waiting for disk attachment to become active ..."))
	stateChange = StateChangeConf{
		Pending:   []string{"inactive"},
		Target:    []string{"active"},
		Refresh:   DiskAttachmentStateRefreshFunc(conn, vmID, diskID),
		StepState: state,
	}
	if _, err = WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("failed waiting for disk attachment (%s) to become active: %s", diskID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepAddDisk) Cleanup(state multistep.StateBag) {}
