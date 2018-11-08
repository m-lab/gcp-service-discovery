// Package discovery defines interfaces for service discovery.
package discovery

import (
	"context"
)

//- Legacy Interfaces -//

// Source defines the interface for collecting targets from various
// services. New services should implement this interface.
type Source interface {
	// Collect retrieves all targets from a source.
	Collect() error

	// Save writes the targets to the named file.
	Save() error
}

// Factory defines the interface for creating new Source instances.
type Factory interface {
	// Create creates a new Source ready for collection.
	Create() (Source, error)
}

//- New Interfaces -//

// Service collects target configurations for the discovery Manager.
type Service interface {
	// Discover identifies and returns StaticConfig targets from a third-party
	// service.
	Discover(ctx context.Context) ([]StaticConfig, error)
}

// StaticConfig represents a set of targets and associated labels. StaticConfig
// serializes to the "file_sd_config" format.
// https://prometheus.io/docs/prometheus/latest/configuration/configuration/#<file_sd_config>
type StaticConfig struct {
	// Targets is a list of targets identified by a label set. Each target is
	// uniquely identifiable in the group by its address label.
	Targets []string `json:"targets"`

	// Labels is a set of keys/values that are common across all targets in the
	// StaticConfig.
	Labels map[string]string `json:"labels,omitempty"`
}
