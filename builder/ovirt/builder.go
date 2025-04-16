package ovirt

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	ovirtsdk4 "github.com/ovirt/go-ovirt"
)

// BuilderID defines the unique id for the builder.
const BuilderID = "ganto.ovirt"

// Builder is a builder implementation that creates oVirt custom images.
type Builder struct {
	config Config
	runner multistep.Runner
}

var pluginVersion = "0.0.1"

func (b *Builder) ConfigSpec() hcldec.ObjectSpec { return b.config.FlatMapstructure().HCL2Spec() }

// Prepare processes the build configuration parameters.
func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	c, warnings, errs := NewConfig(raws...)
	if errs != nil {
		return nil, warnings, errs
	}
	b.config = *c

	return nil, nil, nil
}

// Run is the main function executing the image build.
func (b *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {
	var err error

	conn, err := ovirtsdk4.NewConnectionBuilder().
		URL(b.config.ovirtURL.String()).
		Username(b.config.Username).
		Password(b.config.Password).
		Insecure(b.config.SkipCertValidation).
		Compress(true).
		Timeout(time.Second * 10).
		Build()
	if err != nil {
		return nil, fmt.Errorf("oVirt: Connection failed, reason: %s", err.Error())
	}

	defer conn.Close()

	log.Printf("Successfully connected to %s\n", b.config.ovirtURL.String())

	// Set up the state
	state := new(multistep.BasicStateBag)
	state.Put("config", &b.config)
	state.Put("conn", conn)
	state.Put("hook", hook)
	state.Put("ui", ui)

	cResp, err := conn.SystemService().
		ClustersService().
		List().
		Send()
	if err != nil {
		return nil, fmt.Errorf("Error getting cluster list: %w", err)
	}
	var clusterID string
	if clusters, ok := cResp.Clusters(); ok {
		for _, cluster := range clusters.Slice() {
			if clusterName, ok := cluster.Name(); ok {
				if clusterName == b.config.Cluster {
					clusterID = cluster.MustId()
					log.Printf("Using cluster id: %s", clusterID)
					break
				}
			}
		}
	}
	if clusterID == "" {
		return nil, fmt.Errorf("Could not find cluster '%s'", b.config.Cluster)
	}
	state.Put("cluster_id", clusterID)

	// Build the steps
	steps := []multistep.Step{}
	steps = append(steps, &stepKeyPair{
		Debug:        b.config.PackerDebug,
		Comm:         &b.config.Comm,
		DebugKeyPath: fmt.Sprintf("ovirt_%s.pem", b.config.PackerBuildName),
	},
	)
	steps = append(steps, &stepCreateVM{})
	if b.config.SourceType == ISOSource {
		// Assuming that builds starting from a template already have a boot disk.
		steps = append(steps, &stepAddDisk{})
	}
	steps = append(steps, &stepAddNic{})
	steps = append(steps, &stepSetupInitialRun{
		Debug: b.config.PackerDebug,
		Comm:  &b.config.Comm,
	})
	if b.config.SourceType == ISOSource {
		steps = append(steps, &stepBootCommand{})
	}
	steps = append(steps, &communicator.StepConnect{
		Config:    &b.config.Comm,
		Host:      commHost,
		SSHConfig: b.config.Comm.SSHConfigFunc(),
	},
	)
	steps = append(steps, &commonsteps.StepProvision{})
	steps = append(steps, &commonsteps.StepCleanupTempKeys{
		Comm: &b.config.Comm,
	},
	)
	steps = append(steps, &stepStopVM{})
	steps = append(steps, &stepUpdateDisk{})
	if len(b.config.TemplateName) != 0 {
		steps = append(steps, &stepCreateTemplate{})
	}
	if len(b.config.DiskName) != 0 {
		steps = append(steps, &stepDetachDisk{})
	}

	// To use `Must` methods, you should recover it if panics
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("oVirt: Panics occurs, try the non-Must methods to find the reason (%s)", err)
		}
	}()

	// Configure the runner and run the steps
	b.runner = commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	b.runner.Run(ctx, state)

	// If there was an error, return that
	if rawErr, ok := state.GetOk("error"); ok {
		return nil, rawErr.(error)
	}

	// If there are no images, then just return
	if _, ok := state.GetOk("disk_id"); !ok {
		return nil, nil
	}

	// Build the artifact and return it
	artifact := &Artifact{
		diskID: state.Get("disk_id").(string),
	}

	return artifact, nil
}
