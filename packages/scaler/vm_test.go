package scaler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/actions/scaleset"
)

func TestAllowDeleteAfterIdentityMismatchViaScaleSet(t *testing.T) {
	cfg := newTestConfig()
	vm := ManagedRunnerVM{Name: "scalefleet-ci-runner-1"}
	client := &fakeScaleSetClient{
		getRunnerFn: func(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error) {
			return &scaleset.RunnerReference{Name: "scalefleet-ci-runner-1", ID: runnerID}, nil
		},
	}

	allow, reason, err := allowDeleteAfterIdentityMismatch(context.Background(), cfg, client, vm, "scalefleet-ci-runner-1", 101)
	if err != nil {
		t.Fatalf("allowDeleteAfterIdentityMismatch returned error: %v", err)
	}
	if !allow {
		t.Fatal("expected delete to be allowed")
	}
	if reason != "validated_via_scaleset_runner_reference" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestAllowDeleteAfterIdentityMismatchFallsBackToMetadataMarker(t *testing.T) {
	cfg := newTestConfig()
	vm := ManagedRunnerVM{
		Name:         "scalefleet-ci-runner-1",
		HasJITConfig: true,
	}
	client := &fakeScaleSetClient{
		getRunnerFn: func(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error) {
			return nil, errors.New("not found")
		},
		getRunnerByNameFn: func(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error) {
			return nil, errors.New("not found")
		},
	}

	allow, reason, err := allowDeleteAfterIdentityMismatch(context.Background(), cfg, client, vm, "scalefleet-ci-runner-1", 101)
	if err != nil {
		t.Fatalf("allowDeleteAfterIdentityMismatch returned error: %v", err)
	}
	if !allow {
		t.Fatal("expected delete to be allowed by metadata fallback")
	}
	if reason != "validated_via_instance_name_and_jit_marker" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestAllowDeleteAfterIdentityMismatchRejectsNameMismatch(t *testing.T) {
	cfg := newTestConfig()
	vm := ManagedRunnerVM{Name: "scalefleet-ci-runner-other"}

	allow, reason, err := allowDeleteAfterIdentityMismatch(context.Background(), cfg, &fakeScaleSetClient{}, vm, "scalefleet-ci-runner-1", 101)
	if err != nil {
		t.Fatalf("allowDeleteAfterIdentityMismatch returned error: %v", err)
	}
	if allow {
		t.Fatal("expected delete to be rejected")
	}
	if reason != "" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestValidateRunnerViaScaleSetFallsBackToNameLookup(t *testing.T) {
	cfg := newTestConfig()
	client := &fakeScaleSetClient{
		getRunnerFn: func(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error) {
			return nil, errors.New("by-id unavailable")
		},
		getRunnerByNameFn: func(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error) {
			return &scaleset.RunnerReference{Name: runnerName, ID: 303}, nil
		},
	}

	ok, err := validateRunnerViaScaleSet(context.Background(), cfg, client, "scalefleet-ci-runner-1", 303)
	if err != nil {
		t.Fatalf("validateRunnerViaScaleSet returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected validation success")
	}
}

func TestValidateRunnerViaScaleSetDetectsMismatch(t *testing.T) {
	cfg := newTestConfig()
	client := &fakeScaleSetClient{
		getRunnerFn: func(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error) {
			return &scaleset.RunnerReference{Name: "scalefleet-ci-runner-other", ID: runnerID}, nil
		},
	}

	ok, err := validateRunnerViaScaleSet(context.Background(), cfg, client, "scalefleet-ci-runner-1", 303)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if ok {
		t.Fatal("expected validation failure")
	}
	if !strings.Contains(err.Error(), "runner name mismatch via ID lookup") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRunnerIdentity(t *testing.T) {
	tests := []struct {
		name        string
		vm          ManagedRunnerVM
		expectedErr string
	}{
		{
			name: "exact match",
			vm: ManagedRunnerVM{
				RunnerName:  "scalefleet-ci-runner-1",
				RunnerID:    77,
				HasIdentity: true,
			},
			expectedErr: "",
		},
		{
			name: "name mismatch",
			vm: ManagedRunnerVM{
				RunnerName:  "scalefleet-ci-runner-other",
				RunnerID:    77,
				HasIdentity: true,
			},
			expectedErr: "identity mismatch",
		},
		{
			name: "id mismatch",
			vm: ManagedRunnerVM{
				RunnerName:  "scalefleet-ci-runner-1",
				RunnerID:    88,
				HasIdentity: true,
			},
			expectedErr: "identity mismatch",
		},
		{
			name: "missing identity",
			vm: ManagedRunnerVM{
				RunnerName:  "scalefleet-ci-runner-1",
				HasIdentity: false,
			},
			expectedErr: "identity mismatch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRunnerIdentity(tc.vm, "scalefleet-ci-runner-1", 77)
			if tc.expectedErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectedErr)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStartupScriptBootstrapsRunnerFromJITMetadata(t *testing.T) {
	if !strings.Contains(startupScript, "actions/runner/releases/latest") {
		t.Fatal("startup script must resolve latest GitHub runner release at boot")
	}
	if !strings.Contains(startupScript, "./run.sh --jitconfig") {
		t.Fatal("startup script must run runner with jit config")
	}
	if !strings.Contains(startupScript, "/var/lib/github-runner/jit-config") {
		t.Fatal("startup script must persist jit config to runner state path")
	}
	if strings.Contains(startupScript, "github-runner.service") {
		t.Fatal("startup script must not require pre-baked GitHub runner systemd service")
	}
}

func TestIsZoneCapacityError(t *testing.T) {
	if !isZoneCapacityError(errors.New("ZONE_RESOURCE_POOL_EXHAUSTED")) {
		t.Fatal("expected zone capacity error detection for ZONE_RESOURCE_POOL_EXHAUSTED")
	}
	if !isZoneCapacityError(errors.New("ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS")) {
		t.Fatal("expected zone capacity error detection for ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS")
	}
	if !isZoneCapacityError(errors.New("reason: cpu_availability")) {
		t.Fatal("expected zone capacity error detection for cpu_availability")
	}
	if isZoneCapacityError(errors.New("permission denied")) {
		t.Fatal("unexpected capacity classification for unrelated error")
	}
}
