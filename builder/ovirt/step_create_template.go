package ovirt

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepCreateTemplate struct{}

func (s *stepCreateTemplate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	temp, ok := state.GetOk("vm_id")
	if !ok {
		return multistep.ActionContinue
	}
	vmID := temp.(string)

	// Delete template with the same name if it already exists.
	// We've already provisioned the new VM so we're fairly sure creating the new template will work.
	ui.Message(fmt.Sprintf("Checking for existing template %s ...", config.TemplateName))
	templatesService := conn.SystemService().TemplatesService()
	log.Printf("Searching for existing template '%s'", config.TemplateName)
	tpsResp, err := templatesService.List().
		Search(fmt.Sprintf("name=%s", config.TemplateName)).
		Send()
	if err != nil {
		err = fmt.Errorf("error searching templates: %s", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	tpSlice, _ := tpsResp.Templates()

	for _, tp := range tpSlice.Slice() {
		_, err := templatesService.TemplateService(tp.MustId()).Remove().Send()
		if err != nil {
			err = fmt.Errorf("could not remove template '%s'", config.TemplateName)
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}

	// Remove VM payloads if they were set during the initial run step.
	if _, ok := state.GetOk("payload_set"); ok {
		vmService := conn.SystemService().VmsService().VmService(vmID)
		vmService.Update().Vm(ovirtsdk4.NewVmBuilder().Payloads(&ovirtsdk4.PayloadSlice{}).MustBuild()).Send()
	}

	// We need to build a new VM as the object contained in the template should only contain the ID.
	vm := ovirtsdk4.NewVmBuilder().Id(vmID).MustBuild()

	templateBuilder := ovirtsdk4.NewTemplateBuilder()
	templateBuilder.Vm(vm)
	templateBuilder.Name(config.TemplateName)
	templateBuilder.Description(config.TemplateDescription)
	template, err := templateBuilder.Build()
	if err != nil {
		err = fmt.Errorf("could not build template object: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Message(fmt.Sprintf("Creating template %s ...", config.TemplateName))
	templateResp, err := conn.SystemService().TemplatesService().Add().Template(template).Send()
	if err != nil {
		err = fmt.Errorf("could not create template from VM: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	template = templateResp.MustTemplate()

	// Our temporary VM will be locked again while the template is being created.
	ui.Message(fmt.Sprintf("Waiting for temporary virtual machine to become ready (status down) ..."))
	stateChange := StateChangeConf{
		Pending:   []string{string(ovirtsdk4.VMSTATUS_IMAGE_LOCKED)},
		Target:    []string{string(ovirtsdk4.VMSTATUS_DOWN)},
		Refresh:   VMStateRefreshFunc(conn, vmID),
		StepState: state,
	}
	if _, err := WaitForState(&stateChange); err != nil {
		err = fmt.Errorf("failed waiting for VM (%s) to become down: %w", vmID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	_, err = conn.SystemService().VmsService().VmService(vmID).Remove().Send()
	if err != nil {
		err = fmt.Errorf("could not remove temporary virtual machine: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionContinue
	}

	return multistep.ActionContinue
}

func (s *stepCreateTemplate) Cleanup(state multistep.StateBag) {}
