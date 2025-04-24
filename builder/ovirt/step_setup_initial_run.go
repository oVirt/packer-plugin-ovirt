package ovirt

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type stepSetupInitialRun struct {
	Debug bool
	Comm  *communicator.Config
}

func (s *stepSetupInitialRun) findImage(conn *ovirtsdk4.Connection, name string) (string, error) {
	log.Printf("Searching for ISO image '%s'", name)
	service := conn.SystemService().DisksService()
	resp, err := service.List().Search(fmt.Sprintf("disk_content_type=iso and name=%s", name)).Send()
	if err != nil {
		return "", fmt.Errorf("error while searching for storage domain '%s': %s", name, err)
	}
	disks := resp.MustDisks().Slice()

	if len(disks) == 0 {
		return "", fmt.Errorf("could not find ISO image '%s'", name)
	}
	return disks[0].MustId(), nil
}

func (s *stepSetupInitialRun) readFile(name string) (*ovirtsdk4.File, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("could not read cd_file %s: %w", name, err)
	}
	fileBuilder := ovirtsdk4.NewFileBuilder()
	fileBuilder.Content(fmt.Sprintf(`<![CDATA[%s]]>`, string(content)))
	fileBuilder.Name(name)
	return fileBuilder.MustBuild(), nil
}

// Run executes the Packer build step that configures the initial run setup
func (s *stepSetupInitialRun) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Say("Setting up initial run...")

	vmID := state.Get("vm_id").(string)
	vmService := conn.SystemService().VmsService().VmService(vmID)

	vmBuilder := ovirtsdk4.NewVmBuilder()
	initBuilder := ovirtsdk4.NewInitializationBuilder()

	log.Println(config.SourceType)

	if s.Comm.SSHUsername != "" {
		log.Printf("Set SSH user name: %s", s.Comm.SSHUsername)
		initBuilder.UserName(s.Comm.SSHUsername)
	}
	if string(s.Comm.SSHPublicKey) != "" {
		publicKey := s.Comm.SSHPublicKey
		log.Printf("Set authorized SSH key: %s", string(publicKey))
		initBuilder.AuthorizedSshKeys(string(publicKey))
	}

	if len(config.IPAddress) > 0 {
		log.Printf("Set static IP address: %s/%s", config.IPAddress, config.Netmask)
		log.Printf("Set gateway: %s", config.Gateway)

		ipBuilder := ovirtsdk4.NewIpBuilder()
		ipBuilder.Address(config.IPAddress)
		ipBuilder.Netmask(config.Netmask)
		ipBuilder.Gateway(config.Gateway)

		nicBuilder := ovirtsdk4.NewNicConfigurationBuilder()
		nicBuilder.Name(config.NicName)
		nicBuilder.BootProtocol(ovirtsdk4.BootProtocol("static"))
		nicBuilder.OnBoot(true)
		nicBuilder.IpBuilder(ipBuilder)

		initBuilder.NicConfigurationsOfAny(nicBuilder.MustBuild())
	}

	if config.SourceType == ISOSource {
		isoID, err := s.findImage(conn, config.SourceISO)
		if err != nil {
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}

		// Attach the boot CDROM to the VM.
		cdromBuilder := ovirtsdk4.NewCdromBuilder()
		cdromBuilder.File(ovirtsdk4.NewFileBuilder().Id(isoID).MustBuild())

		bootBuilder := ovirtsdk4.NewBootBuilder()
		bootBuilder.DevicesOfAny(ovirtsdk4.BOOTDEVICE_CDROM)

		osBuilder := ovirtsdk4.NewOperatingSystemBuilder()
		osBuilder.BootBuilder(bootBuilder)

		vmBuilder.OsBuilder(osBuilder)

		// TODO: The `current` parameter mounts the cdrom for the next boot. Does this work with installs that require a reboot?
		cdromService := vmService.CdromsService()
		if _, err := cdromService.Add().Cdrom(cdromBuilder.MustBuild()).Query("current", "true").Send(); err != nil {
			err = fmt.Errorf("could not attach CDROM: %s", err)
			ui.Error(err.Error())
			state.Put("error", err)
			return multistep.ActionHalt
		}

		displayBuilder := ovirtsdk4.NewDisplayBuilder()
		displayBuilder.Type(ovirtsdk4.DISPLAYTYPE_VNC)
		displayBuilder.Monitors(1)
		vmBuilder.DisplayBuilder(displayBuilder)

		if len(config.CDFiles) > 0 || len(config.CDContent) > 0 {
			cdBuilder := ovirtsdk4.NewPayloadBuilder()
			cdBuilder.Type(ovirtsdk4.VMDEVICETYPE_CDROM)
			cdBuilder.VolumeId(config.CDName)

			files := make([]*ovirtsdk4.File, 0, len(config.CDFiles)+len(config.CDContent))

			// Add files from local (packer machine) folder
			for _, name := range config.CDFiles {
				stat, err := os.Stat(name)
				if err != nil {
					err := fmt.Errorf("could not stat cd_file %s: %w", name, err)
					state.Put("error", err)
					ui.Error(err.Error())
					return multistep.ActionHalt
				}
				if stat.IsDir() {
					entries, err := os.ReadDir(name)
					if err != nil {
						err := fmt.Errorf("could not read file list for directory '%s': %w", name, err)
						state.Put("error", err)
						ui.Error(err.Error())
						return multistep.ActionHalt
					}
					for i := range entries {
						fileName := entries[i].Name()
						filePath := fmt.Sprintf("%s/%s", name, fileName)
						file, err := s.readFile(filePath)
						if err != nil {
							state.Put("error", err)
							ui.Error(err.Error())
							return multistep.ActionHalt
						}
						file.SetName(fileName)
						files = append(files, file)
					}
				} else {
					file, err := s.readFile(name)
					if err != nil {
						state.Put("error", err)
						ui.Error(err.Error())
						return multistep.ActionHalt
					}
					files = append(files, file)
				}
			}

			// Add files with given content
			for name, content := range config.CDContent {
				fileBuilder := ovirtsdk4.NewFileBuilder()
				fileBuilder.Content(fmt.Sprintf(`<![CDATA[%s]]>`, content))
				fileBuilder.Name(name)
				files = append(files, fileBuilder.MustBuild())
			}

			payload := ovirtsdk4.NewPayloadBuilder().FilesOfAny(files...).VolumeId(config.CDName).Type(ovirtsdk4.VMDEVICETYPE_CDROM).MustBuild()

			// For some reason the payloads are not added to the VM when using run-once config.
			// vmBuilder.PayloadsOfAny(payload)
			//
			// We have to add them permanently using a VM update:
			if _, err := vmService.Update().Vm(ovirtsdk4.NewVmBuilder().PayloadsOfAny(payload).MustBuild()).Send(); err != nil {
				err := fmt.Errorf("could not attach payload CDROM: %s", err)
				ui.Error(err.Error())
				state.Put("error", err)
				return multistep.ActionHalt
			}

			// Payloads will be removed in the create template step.
			state.Put("payload_set", struct{}{})
		}
	}

	ui.Say("Starting virtual machine (run_once)...")

	vmBuilder.InitializationBuilder(initBuilder)
	vm := vmBuilder.MustBuild()

	// TODO: UseCloudInit won't work for Windows
	startReq := vmService.Start().Vm(vm)
	if len(config.CDFiles) == 0 && len(config.CDContent) == 0 {
		// Using CloudInit/Sysprep in oVirt overwrites the virtual CDROM containing user-supplied sysprep files.
		// We should only enable this when no files were supplied.
		if config.CloudInit {
			startReq.UseCloudInit(true)
		} else if config.SysPrep {
			startReq.UseSysprep(true)
		}
	}
	if _, err := startReq.Send(); err != nil {
		err = fmt.Errorf("could not start VM: %s", err)
		ui.Error(err.Error())
		state.Put("error", err)
		return multistep.ActionHalt
	}

	ui.Message(fmt.Sprintf("Waiting for VM to become ready ..."))
	stateChange := StateChangeConf{
		Pending:   []string{string(ovirtsdk4.VMSTATUS_WAIT_FOR_LAUNCH), string(ovirtsdk4.VMSTATUS_POWERING_UP)},
		Target:    []string{string(ovirtsdk4.VMSTATUS_UP)},
		Refresh:   VMStateRefreshFunc(conn, vmID),
		StepState: state,
	}

	// oVirt does not know when the VM is ready when there's no guest agent, it defaults to 60 seconds.
	// This is too long for the Debian Installer, it goes into Speech Synthesis mode automatically.
	// We fix this by continuing to the next Packer step immediately after the VM goes to powering_up state.
	if config.BootCommand.VNCConfig.BootWait != 0 {
		stateChange.Pending = []string{string(ovirtsdk4.VMSTATUS_WAIT_FOR_LAUNCH)}
		stateChange.Target = []string{string(ovirtsdk4.VMSTATUS_POWERING_UP)}
	}

	if _, err := WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("Failed waiting for VM (%s) to become up: %s", vmID, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	ui.Message("VM successfully started!")

	return multistep.ActionContinue
}

// Cleanup any resources that may have been created during the Run phase.
func (s *stepSetupInitialRun) Cleanup(state multistep.StateBag) {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	conn, err := ovirtConnect(config, state)
	if err != nil {
		ui.Error(err.Error())
	}

	vmService := conn.SystemService().VmsService().VmService(vmID)

	// It's not possible to delete VMs that are still running.
	// We have to make sure it's stopped here as the create_vm cleanup step
	// will try to delete it later.
	if _, err := vmService.Stop().Send(); err != nil {
		err = fmt.Errorf("could not stop VM: %s", err)
		ui.Error(err.Error())
	}

	ui.Message(fmt.Sprintf("Waiting for VM to shut down ..."))
	stateChange := StateChangeConf{
		Pending:   []string{string(ovirtsdk4.VMSTATUS_UP), string(ovirtsdk4.VMSTATUS_POWERING_DOWN)},
		Target:    []string{string(ovirtsdk4.VMSTATUS_DOWN)},
		Refresh:   VMStateRefreshFunc(conn, vmID),
		StepState: state,
	}
	if _, err := WaitForState(&stateChange); err != nil {
		err := fmt.Errorf("Failed waiting for VM (%s) to shut down: %s", vmID, err)
		ui.Error(err.Error())
	}
}
