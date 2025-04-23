package ovirt

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/alexsnet/go-vnc"
	"github.com/alexsnet/go-vnc/keys"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt/v4"
)

type adaptor struct {
	c *vnc.ClientConn
}

func (a *adaptor) KeyEvent(key uint32, down bool) error {
	return a.c.KeyEvent(keys.Key(key), down)
}

type BootCommandConfig struct {
	VNCConfig   bootcommand.VNCConfig `mapstructure:",squash"`
	VNCPassword string                `mapstructure:"vnc_password"`
	VNCIP       string                `mapstructure:"vnc_ip"`
	VNCPort     int                   `mapstructure:"vnc_port"`
}

type stepBootCommand struct{}

// Run executes the Packer build step that configures the initial run setup
func (s *stepBootCommand) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	conn := state.Get("conn").(*ovirtsdk4.Connection)
	ui := state.Get("ui").(packer.Ui)
	vmID := state.Get("vm_id").(string)

	if len(config.BootCommand.VNCConfig.BootCommand) == 0 {
		ui.Say("No boot command provided, skipping...")
		return multistep.ActionContinue
	}

	vmService := conn.SystemService().VmsService().VmService(vmID)
	vmResp, err := vmService.Get().Send()
	if err != nil {
		err = fmt.Errorf("could not get VM details: %s", err)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}
	vm := vmResp.MustVm()

	display, ok := vm.Display()
	if !ok {
		err = fmt.Errorf("could not get display details for VM %s (not included in vm details)", vmID)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}

	if config.BootCommand.VNCConfig.BootWait > 0 {
		ui.Sayf("Waiting %s for boot...", config.BootCommand.VNCConfig.BootWait.String())
		select {
		case <-time.After(config.BootCommand.VNCConfig.BootWait):
			break
		case <-ctx.Done():
			log.Println("fucked")
			return multistep.ActionHalt
		}
	}

	vncIP, ok := display.Address()
	if !ok {
		err = fmt.Errorf("no VNC address provided by oVirt")
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}
	vncPort, ok := display.Port()
	if !ok {
		err = fmt.Errorf("no VNC port provided by oVirt")
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}

	hostPort := net.JoinHostPort(vncIP, strconv.FormatInt(vncPort, 10))

	log.Printf("Connecting to VNC on %s", hostPort)

	vncConn, err := net.Dial("tcp", hostPort)
	if err != nil {
		err = fmt.Errorf("could not connect to VNC host: %w", err)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}
	defer vncConn.Close()

	ticketResp, err := vmService.Ticket().Send()
	if err != nil {
		err = fmt.Errorf("could not get VNC ticket: %w", err)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}
	ticket := ticketResp.MustTicket()

	log.Printf("VNC ticket %s (%d)", ticket.MustValue(), ticket.MustExpiry())

	vncConfig := vnc.NewClientConfig(ticket.MustValue())
	vncClient, err := vnc.Connect(ctx, vncConn, vncConfig)
	if err != nil {
		err = fmt.Errorf("could not talk to VNC service: %w", err)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}
	defer vncClient.Close()

	a := &adaptor{c: vncClient}

	command := config.BootCommand.VNCConfig.FlatBootCommand()

	ui.Sayf("Typing boot command: %s", command)

	driver := bootcommand.NewVNCDriver(a, config.BootCommand.VNCConfig.BootKeyInterval)

	seq, err := bootcommand.GenerateExpressionSequence(command)
	if err != nil {
		err = fmt.Errorf("could not generate expression sequence: %w", err)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}

	if err := seq.Do(ctx, driver); err != nil {
		err = fmt.Errorf("could not execute boot command: %w", err)
		ui.Error(err.Error())
		state.Put("error", err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

// Cleanup any resources that may have been created during the Run phase.
func (s *stepBootCommand) Cleanup(state multistep.StateBag) {}
