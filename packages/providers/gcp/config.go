package gcp

import (
	"strings"
	"time"

	"github.com/LexLuthr/scalefleet/packages/scaler"
	"golang.org/x/xerrors"
)

// Config defines Google Cloud provider settings for VM lifecycle and secret access.
type Config struct {
	// ProjectID is the Google Cloud project used for compute and secrets operations.
	ProjectID string
	// Zones is the ordered list of zones used for runner VM placement and lookup.
	Zones []string
	// Runner defines runner VM image, machine, network, and startup behavior.
	Runner RunnerConfig
}

// RunnerConfig defines runner VM image and networking behavior on GCP.
type RunnerConfig struct {
	// Image is the source image path used to create runner VMs.
	Image string
	// MachineType is the exact machine type used when creating runner VMs.
	MachineType string
	// Network is the network resource path attached to runner VMs.
	Network string
	// ServiceAccountEmail is the service account email attached to runner VMs.
	ServiceAccountEmail string
	// NetworkTags is the optional list of network tags attached to runner VMs.
	NetworkTags []string
	// Script is the startup script passed through VM metadata.
	Script string
	// RunnerMaxRunDuration is the hard max runtime applied to runner VMs.
	RunnerMaxRunDuration time.Duration
}

// DefaultConfig returns baseline GCP provider values initialized from scaler defaults.
func DefaultConfig() *Config {
	return &Config{
		Runner: RunnerConfig{
			Script:               scaler.DefaultRunnerStartupScript(),
			RunnerMaxRunDuration: scaler.DefaultRunnerMaxRunDuration(),
		},
	}
}

// MachineType returns a normalized machine type used by core naming and metadata.
func (cfg *Config) MachineType() string {
	if cfg == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(cfg.Runner.MachineType))
}

// Validate checks required provider settings.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return xerrors.New("provider config is nil")
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return xerrors.New("project-id is required")
	}
	if strings.TrimSpace(cfg.Runner.Image) == "" {
		return xerrors.New("runner-image is required")
	}
	if cfg.MachineType() == "" {
		return xerrors.New("runner-machine-type is required")
	}
	if strings.TrimSpace(cfg.Runner.Network) == "" {
		return xerrors.New("runner-network is required")
	}
	if strings.TrimSpace(cfg.Runner.ServiceAccountEmail) == "" {
		return xerrors.New("runner-sa-email is required")
	}
	if len(cfg.Zones) == 0 {
		return xerrors.New("gcp.zones is required")
	}
	for _, zone := range cfg.Zones {
		if strings.TrimSpace(zone) == "" {
			return xerrors.New("gcp.zones cannot contain empty values")
		}
	}
	for _, tag := range cfg.Runner.NetworkTags {
		if strings.TrimSpace(tag) == "" {
			return xerrors.New("runner-network-tag cannot contain empty values")
		}
	}
	if cfg.Runner.RunnerMaxRunDuration <= 0 {
		return xerrors.New("gcp.runner.runnerMaxRunDuration must be > 0")
	}
	return nil
}

// RunnerMaxRunDuration returns provider runner hard-limit duration for runtime derivation.
func (cfg *Config) RunnerMaxRunDuration() time.Duration {
	if cfg == nil {
		return 0
	}
	return cfg.Runner.RunnerMaxRunDuration
}
