//go:generate go tool github.com/hashicorp/packer-plugin-sdk/cmd/packer-sdc mapstructure-to-hcl2 -type Config
package ovirt

import (
	"fmt"
	"log"
	"slices"

	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"github.com/hashicorp/packer-plugin-sdk/uuid"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`

	AccessConfig `mapstructure:",squash"`
	SourceConfig `mapstructure:",squash"`

	Comm        communicator.Config `mapstructure:",squash"`
	BootCommand BootCommandConfig   `mapstructure:",squash"`

	VMName    string `mapstructure:"vm_name"`
	IPAddress string `mapstructure:"address"`
	Netmask   string `mapstructure:"netmask"`
	Gateway   string `mapstructure:"gateway"`
	NicName   string `mapstructure:"nic_name"`

	DiskName        string `mapstructure:"disk_name"`
	DiskDescription string `mapstructure:"disk_description"`
	DiskSize        uint64 `mapstructure:"disk_size"`

	Cores           uint64 `mapstructure:"cpu_cores"`
	Sockets         uint64 `mapstructure:"cpu_sockets"`
	Memory          uint64 `mapstructure:"memory"` // In MB, same as vSphere plugin.
	OperatingSystem string `mapstructure:"operating_system"`

	TemplateName        string `mapstructure:"template_name"`
	TemplateDescription string `mapstructure:"template_description"`

	Network       string `mapstructure:"network"`
	VNICProfile   string `mapstructure:"vnic_profile"`
	StorageDomain string `mapstructure:"storage_domain"`
	BiosType      string `mapstructure:"bios_type"`

	ctx interpolate.Context
}

func NewConfig(raws ...interface{}) (*Config, []string, error) {
	c := new(Config)

	err := config.Decode(c, &config.DecodeOpts{
		Interpolate:        true,
		InterpolateContext: &c.ctx,
	}, raws...)
	if err != nil {
		return nil, nil, err
	}

	// Accumulate any errors
	var errs *packer.MultiError
	errs = packer.MultiErrorAppend(errs, c.AccessConfig.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.SourceConfig.Prepare(&c.ctx)...)

	if len(c.Network) == 0 && len(c.VNICProfile) != 0 {
		c.Network = c.VNICProfile
		log.Printf("Set network to %s (copy from VNICProfile)", c.Network)
	} else if len(c.VNICProfile) == 0 && len(c.Network) != 0 {
		c.VNICProfile = c.Network
		log.Printf("Set VNICProfile to %s (copy from network)", c.VNICProfile)
	}

	if c.VMName == "" {
		// Default to packer-[time-ordered-uuid]
		c.VMName = fmt.Sprintf("packer-%s", uuid.TimeOrderedUUID())
	}
	if c.DiskName == "" {
		c.DiskName = c.VMName
	}
	if c.Netmask == "" {
		c.Netmask = "255.255.255.0"
		log.Printf("Set default netmask to %s", c.Netmask)
	}
	if c.NicName == "" {
		c.NicName = "enp1s0"
		log.Printf("Set default nic name to %s", c.NicName)
	}

	if c.Cores == 0 {
		c.Cores = 1
		log.Printf("Set default cpu cores to %d", c.Cores)
	}
	if c.Sockets == 0 {
		c.Sockets = 1
		log.Printf("Set default cpu sockets to %d", c.Sockets)
	}
	if c.Memory == 0 {
		c.Memory = 2048
		log.Printf("Set default memory to %d MB", c.Memory)
	}

	if len(c.BiosType) > 0 {
		options := []ovirtsdk4.BiosType{
			ovirtsdk4.BIOSTYPE_I440FX_SEA_BIOS,
			ovirtsdk4.BIOSTYPE_Q35_OVMF,
			ovirtsdk4.BIOSTYPE_Q35_SEA_BIOS,
			ovirtsdk4.BIOSTYPE_Q35_SECURE_BOOT,
		}
		if !slices.Contains(options, ovirtsdk4.BiosType(c.BiosType)) {
			errs = packer.MultiErrorAppend(errs, fmt.Errorf("Invalid bios_type: %s", c.BiosType))
		}
	}

	errs = packer.MultiErrorAppend(errs, c.Comm.Prepare(&c.ctx)...)

	if errs != nil && len(errs.Errors) > 0 {
		return nil, nil, errs
	}

	packer.LogSecretFilter.Set(c.Password)
	return c, nil, nil
}
