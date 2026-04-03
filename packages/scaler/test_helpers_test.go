package scaler

import (
	"context"
	"errors"
	"time"
)

func newTestConfig() *Config {
	cfg := DefaultConfig()
	cfg.Provider = &testProvider{
		machineType: "e2-standard-8",
		maxRun:      runnerMaxRunDuration,
		listFn: func(ctx context.Context, managedPrefix string) ([]ManagedRunnerVM, error) {
			return []ManagedRunnerVM{}, nil
		},
		createFn: func(ctx context.Context, req CreateRunnerVMRequest) (string, error) {
			return "us-central1-a", nil
		},
		deleteFn: func(ctx context.Context, vm ManagedRunnerVM) error {
			return nil
		},
		secretFn: func(ctx context.Context, secretName string) (string, error) {
			return "", errors.New("secret not configured")
		},
	}
	cfg.Runtime.APICallTimeout = time.Second
	cfg.Runtime.APIInitialBackoff = time.Millisecond
	cfg.Runtime.APIReadAttempts = 1
	cfg.Runtime.APIWriteAttempts = 1
	_ = initRuntimeVars(&cfg)
	return &cfg
}

type testProvider struct {
	machineType string
	maxRun      time.Duration
	validateErr error

	listFn   func(ctx context.Context, managedPrefix string) ([]ManagedRunnerVM, error)
	createFn func(ctx context.Context, req CreateRunnerVMRequest) (string, error)
	deleteFn func(ctx context.Context, vm ManagedRunnerVM) error
	secretFn func(ctx context.Context, secretName string) (string, error)
}

func (p *testProvider) MachineType() string {
	return p.machineType
}

func (p *testProvider) Validate() error {
	return p.validateErr
}

func (p *testProvider) RunnerMaxRunDuration() time.Duration {
	return p.maxRun
}

func (p *testProvider) ListManagedRunnerVMs(ctx context.Context, managedPrefix string) ([]ManagedRunnerVM, error) {
	if p.listFn == nil {
		return nil, errors.New("ListManagedRunnerVMs is not configured")
	}
	return p.listFn(ctx, managedPrefix)
}

func (p *testProvider) CreateRunnerVM(ctx context.Context, req CreateRunnerVMRequest) (string, error) {
	if p.createFn == nil {
		return "", errors.New("CreateRunnerVM is not configured")
	}
	return p.createFn(ctx, req)
}

func (p *testProvider) DeleteRunnerVM(ctx context.Context, vm ManagedRunnerVM) error {
	if p.deleteFn == nil {
		return errors.New("DeleteRunnerVM is not configured")
	}
	return p.deleteFn(ctx, vm)
}

func (p *testProvider) LoadSecretValue(ctx context.Context, secretName string) (string, error) {
	if p.secretFn == nil {
		return "", errors.New("LoadSecretValue is not configured")
	}
	return p.secretFn(ctx, secretName)
}
