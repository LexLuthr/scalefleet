package scaler

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

// CloudProvider is the full provider contract consumed by core runtime flows.
type CloudProvider interface {
	// MachineType returns a normalized machine type value for naming and metadata.
	MachineType() string
	// Validate verifies provider-specific configuration requirements.
	Validate() error
	// RunnerMaxRunDuration returns the hard VM runtime used for scheduler termination settings.
	RunnerMaxRunDuration() time.Duration
	// ListManagedRunnerVMs returns active managed VMs that match the provided managed prefix.
	ListManagedRunnerVMs(ctx context.Context, managedPrefix string) ([]ManagedRunnerVM, error)
	// CreateRunnerVM creates one managed runner VM and returns the chosen placement identifier.
	CreateRunnerVM(ctx context.Context, req CreateRunnerVMRequest) (string, error)
	// DeleteRunnerVM deletes one managed runner VM by provider-resolved identity.
	DeleteRunnerVM(ctx context.Context, vm ManagedRunnerVM) error
	// LoadSecretValue resolves and returns a provider-backed secret value by name.
	LoadSecretValue(ctx context.Context, secretName string) (string, error)
}

// ManagedRunnerVM is the provider-agnostic VM snapshot consumed by core reconcile and cleanup flows.
type ManagedRunnerVM struct {
	// Name is the provider VM instance name.
	Name string
	// Zone is the provider-specific placement identifier for this VM.
	Zone string
	// RunnerName is the GitHub runner name encoded in VM metadata.
	RunnerName string
	// RunnerID is the GitHub runner ID encoded in VM metadata.
	RunnerID int
	// HasIdentity reports whether RunnerName and RunnerID were decoded successfully from metadata.
	HasIdentity bool
	// HasJITConfig reports whether VM metadata contains a non-empty jit-config value.
	HasJITConfig bool
	// CreatedAt is the VM creation timestamp decoded to UTC.
	CreatedAt time.Time
	// HasCreationTime reports whether CreatedAt was decoded successfully.
	HasCreationTime bool
}

// CreateRunnerVMRequest carries create-time runner data from core into provider VM creation.
type CreateRunnerVMRequest struct {
	// JITConfig is the encoded GitHub JIT config payload.
	JITConfig string
	// RunnerName is the exact GitHub runner name to bind to VM metadata.
	RunnerName string
	// RunnerID is the exact GitHub runner ID to bind to VM metadata.
	RunnerID int
	// MaxRunDurationSeconds is the enforced VM max runtime used by provider scheduling config.
	MaxRunDurationSeconds int64
}

// provider returns the configured provider after nil safety checks.
func (cfg *Config) provider() (CloudProvider, error) {
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}
	if cfg.Provider == nil {
		return nil, xerrors.New("provider is required")
	}
	return cfg.Provider, nil
}
