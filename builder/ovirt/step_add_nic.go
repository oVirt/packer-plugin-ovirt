package ovirt

import (
	"context"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"log"
	ovirtsdk4 "github.com/ovirt/go-ovirt"
)

type stepAddNic struct{}

func (s *stepAddNic) findVnicProfile(conn *ovirtsdk4.Connection, networkID string, name string) (string, error) {
	log.Printf("Searching for VNIC profile '%s'", name)
	service := conn.SystemService().NetworksService().NetworkService(networkID).VnicProfilesService()
	resp, err := service.List().Send()
	if err != nil {
		return "", fmt.Errorf("error while searching for VNIC profile '%s': %s", name, err)
	}
	profiles := resp.MustProfiles().Slice()
	if len(profiles) == 0 {
		return "", fmt.Errorf("could not find VNIC profile '%s'", name)
	}
	for i := range profiles {
		if profiles[i].MustName() == name {
			return profiles[i].MustId(), nil
		}
	}
	return profiles[0].MustId(), nil
}

func (s *stepAddNic) findNetwork(conn *ovirtsdk4.Connection, name string) (string, error) {
	log.Printf("Searching network '%s'", name)
	service := conn.SystemService().NetworksService()
	resp, err := service.List().Search(fmt.Sprintf("name=%s", name)).Max(1).Send()
	if err != nil {
		return "", fmt.Errorf("error while searching for network '%s': %s", name, err)
	}
	networks := resp.MustNetworks().Slice()

	if len(networks) == 0 {
		return "", fmt.Errorf("could not find network '%s'", name)
	}

	return networks[0].MustId(), nil
}

func (s *stepAddNic) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	if len(config.Network) == 0 || len(config.VNICProfile) == 0 {
		ui.Say("No NIC config provided, skipping...")
		return multistep.ActionContinue
	}

	vmService := conn.SystemService().VmsService().VmService(vmID)
	nicService := vmService.NicsService()

	ui.Say("Removing existing NICs...")

	// When building from a template, the template may already have a NIC that is configured differently.
	// We'll remove all existing NICs before adding our new one.
	existingResp, err := nicService.List().Send()
	if err != nil {
		err = fmt.Errorf("could not get existing NIC list: %w", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	existing := existingResp.MustNics().Slice()

	for i := range existing {
		nicID := existing[i].MustId()
		log.Printf("Removing NIC %s", nicID)
		if _, err := nicService.NicService(nicID).Remove().Send(); err != nil {
			err = fmt.Errorf("could not remove existing NIC '%s': %w", nicID, err)
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}
	}

	ui.Say("Configuring new NIC...")

	networkID, err := s.findNetwork(conn, config.Network)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}
	vnicProfileID, err := s.findVnicProfile(conn, networkID, config.VNICProfile)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	nicBuilder := ovirtsdk4.NewNicBuilder()
	nicBuilder.Name("bobby")
	nicBuilder.Plugged(true)
	nicBuilder.Interface(ovirtsdk4.NICINTERFACE_VIRTIO)
	nicBuilder.NetworkBuilder(ovirtsdk4.NewNetworkBuilder().Id(networkID))
	nicBuilder.VnicProfileBuilder(ovirtsdk4.NewVnicProfileBuilder().Id(vnicProfileID))
	if _, err := nicService.Add().Nic(nicBuilder.MustBuild()).Send(); err != nil {
		err = fmt.Errorf("could not add NIC: %w", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepAddNic) Cleanup(state multistep.StateBag) {}
