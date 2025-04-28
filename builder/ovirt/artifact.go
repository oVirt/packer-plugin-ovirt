package ovirt

import (
	"fmt"
)

// Artifact is an artifact implementation that contains built disk.
type Artifact struct {
	template bool
	id       string
}

// BuilderId uniquely identifies the builder.
func (*Artifact) BuilderId() string {
	return BuilderID
}

// Files returns the files represented by the artifact. Not used for oVirt.
func (*Artifact) Files() []string {
	return nil
}

// Id returns the disk identifier of the artifact.
func (a *Artifact) Id() string {
	return a.id
}

func (a *Artifact) String() string {
	if a.template {
		return fmt.Sprintf("A template was created: %s", a.id)
	}
	return fmt.Sprintf("A disk was created: %s", a.id)
}

// State returns specific details from the artifact. Not used for oVirt.
func (a *Artifact) State(name string) any {
	return nil
}

// Destroy deletes the artifact. Not used for oVirt.
func (a *Artifact) Destroy() error {
	return nil
}
