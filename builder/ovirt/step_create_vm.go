package ovirt

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt"
)

type stepCreateVM struct{}

func (s *stepCreateVM) findTemplate(conn *ovirtsdk4.Connection, name string, version int64) (string, error) {
	if name == "Blank" {
		log.Println("Defaulting to nil UUID for Blank template.")
		return "00000000-0000-0000-0000-000000000000", nil
	}

	log.Printf("Searching for template '%s' version '%d'\n", name, version)
	service := conn.SystemService().TemplatesService()
	resp, err := service.List().Search(fmt.Sprintf("name=%s", name)).Send()
	if err != nil {
		return "", fmt.Errorf("could not search for template %s: %w", name, err)
	}
	templateSlice := resp.MustTemplates()

	for _, tp := range templateSlice.Slice() {
		if tp.MustVersion().MustVersionNumber() == version {
			return tp.MustId(), nil
		}
	}

	return "", fmt.Errorf("could not find template '%s' version '%d'", name, version)
}

func (s *stepCreateVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	conn := state.Get("conn").(*ovirtsdk4.Connection)
	clusterID := state.Get("cluster_id").(string)

	ui.Say("Creating virtual machine...")

	templateID := config.SourceTemplateID
	if len(templateID) == 0 {
		log.Println("No template ID provided, searching for template...")
		var err error
		if templateID, err = s.findTemplate(conn, config.SourceTemplateName, config.SourceTemplateVersion); err != nil {
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}
	log.Printf("Using template ID: %s", templateID)

	vmBuilder := ovirtsdk4.NewVmBuilder()
	vmBuilder.Name(config.VMName)
	vmBuilder.ClusterBuilder(ovirtsdk4.NewClusterBuilder().Id(clusterID))
	vmBuilder.TemplateBuilder(ovirtsdk4.NewTemplateBuilder().Id(templateID))

	if templateID == "00000000-0000-0000-0000-000000000000" {
		vmBuilder.Type(ovirtsdk4.VMTYPE_SERVER)
		vmBuilder.BiosBuilder(ovirtsdk4.NewBiosBuilder().Type(ovirtsdk4.BIOSTYPE_Q35_SEA_BIOS)) // TODO: config
		vmBuilder.HighAvailabilityBuilder(ovirtsdk4.NewHighAvailabilityBuilder().Enabled(true))
	}

	vm := vmBuilder.MustBuild()

	vmResp, err := conn.SystemService().VmsService().Add().Vm(vm).Send()
	if err != nil {
		if _, ok := err.(*ovirtsdk4.NotFoundError); ok {
			// The template ID is the only reference that can be configured by the user, other data is looked up through the API.
			// Assume that NotFoundErrors are due to a manually entered template ID.
			err = fmt.Errorf("could not find virtual machine template ID '%s'", templateID)
		} else {
			err = fmt.Errorf("error while creating virtual machine: %s", err)
		}
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	var ok bool
	if vm, ok = vmResp.Vm(); !ok {
		return multistep.ActionHalt
	}

	vmID := vm.MustId()
	state.Put("vm_id", vmID)
	log.Printf("Virtual machine ID: %s", vmID)

	ui.Message(fmt.Sprintf("Waiting for VM to become ready (status down) ..."))
	stateChange := StateChangeConf{
		Pending:   []string{"image_locked"},
		Target:    []string{string(ovirtsdk4.VMSTATUS_DOWN)},
		Refresh:   VMStateRefreshFunc(conn, vmID),
		StepState: state,
	}

	if _, err := WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("failed waiting for VM (%s) to become ready (status down): %w", vmID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepCreateVM) Cleanup(state multistep.StateBag) {
	if _, ok := state.GetOk("vm_id"); !ok {
		return
	}

	ui := state.Get("ui").(packer.Ui)
	conn := state.Get("conn").(*ovirtsdk4.Connection)
	vmID := state.Get("vm_id").(string)

	ui.Say(fmt.Sprintf("Deleting virtual machine: %s ...", vmID))

	if _, err := conn.SystemService().VmsService().VmService(vmID).Remove().Send(); err != nil {
		ui.Error(fmt.Sprintf("Error deleting VM '%s', may still be around: %s", vmID, err))
	}
}
