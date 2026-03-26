package ovirt

import (
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

const (
	TemplateSource = "template"
	ISOSource      = "iso"
)

const BlankTemplateUUID = "00000000-0000-0000-0000-000000000000"

// SourceConfig contains the various source properties for an oVirt image
type SourceConfig struct {
	Cluster string `mapstructure:"cluster"`

	SourceType string `mapstructure:"source_type"`

	SourceTemplateName    string `mapstructure:"source_template_name"`
	SourceTemplateVersion int64  `mapstructure:"source_template_version"`
	SourceTemplateID      string `mapstructure:"source_template_id"`

	SourceISO string `mapstructure:"source_iso"`

	CDName    string            `mapstructure:"cd_name"`
	CDFiles   []string          `mapstructure:"cd_files"`
	CDContent map[string]string `mapstructure:"cd_content"`
}

// Prepare performs basic validation on the SourceConfig
func (c *SourceConfig) Prepare(ctx *interpolate.Context) []error {
	var errs []error

	if c.Cluster == "" {
		c.Cluster = "Default"
	}
	if c.CDName == "" {
		c.CDName = "UNATTEND"
	}

	switch c.SourceType {
	case "":
		log.Printf("Using default source_type: %s", c.SourceType)
		c.SourceType = "template"
		fallthrough
	case TemplateSource:
		log.Println("Using template source type")
		if (c.SourceTemplateName != "") && (c.SourceTemplateVersion < 1) {
			c.SourceTemplateVersion = 1
			log.Printf("Using default source_template_version: %d", c.SourceTemplateVersion)
		}
		if c.SourceTemplateID != "" {
			if _, err := uuid.Parse(c.SourceTemplateID); err != nil {
				errs = append(errs, fmt.Errorf("invalid source_template_id: %s", c.SourceTemplateID))
			}
		}
		if (c.SourceTemplateName != "") && (c.SourceTemplateID != "") {
			errs = append(errs, errors.New("conflict: set either source_template_name or source_template_id"))
		}
		if (c.SourceTemplateName == "") && (c.SourceTemplateID == "") {
			errs = append(errs, errors.New("source_template_name or source_template_id must be specified"))
		}

	case ISOSource:
		log.Println("Using ISO source type")
		c.SourceTemplateName = "Blank"
		c.SourceTemplateID = BlankTemplateUUID
		if c.SourceISO == "" {
			errs = append(errs, errors.New("source_iso must be specified"))
		}

	default:
		errs = append(errs, fmt.Errorf("invalid source_type: %s", c.SourceType))
	}

	// Required configurations that will display errors if not set

	if len(errs) > 0 {
		return errs
	}

	return nil
}
