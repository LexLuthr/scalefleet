package scaler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/actions/scaleset"
	"golang.org/x/xerrors"
)

var errRunnerVMNotFound = errors.New("runner VM not found in managed set")

func (cfg *Config) countRunnerVMs(ctx context.Context) (int, error) {
	instances, err := cfg.listManagedRunnerVMs(ctx)
	if err != nil {
		return 0, err
	}
	return len(instances), nil
}

func (cfg *Config) listManagedRunnerVMs(ctx context.Context) ([]ManagedRunnerVM, error) {
	provider, err := cfg.provider()
	if err != nil {
		return nil, err
	}
	return callWithRetry(ctx, cfg, "ListManagedRunnerVMs", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) ([]ManagedRunnerVM, error) {
		return provider.ListManagedRunnerVMs(callCtx, cfg.Runtime.runnerVMPrefix)
	})
}

func (cfg *Config) createRunnerVM(ctx context.Context, jitConfig, name string, runnerID int) (string, error) {
	provider, err := cfg.provider()
	if err != nil {
		return "", err
	}
	return callWithRetry(ctx, cfg, "CreateRunnerVM", cfg.Runtime.APIWriteAttempts, func(callCtx context.Context) (string, error) {
		return provider.CreateRunnerVM(callCtx, CreateRunnerVMRequest{
			JITConfig:             jitConfig,
			RunnerName:            name,
			RunnerID:              runnerID,
			MaxRunDurationSeconds: cfg.Runtime.runnerMaxRunDurationSeconds,
		})
	})
}

func (cfg *Config) deleteRunnerVM(ctx context.Context, client scaleSetAPI, runnerName string, runnerID int) error {
	if !strings.HasPrefix(runnerName, cfg.Runtime.runnerVMPrefix+"-") {
		return xerrors.Errorf("runner name %q does not match managed VM prefix", runnerName)
	}

	vm, err := cfg.getRunnerVMByName(ctx, runnerName)
	if err != nil {
		if errors.Is(err, errRunnerVMNotFound) {
			log.Warnw("Runner VM already absent on completion", "runnerName", runnerName, "runnerID", runnerID)
			return nil
		}
		return err
	}

	if err := validateRunnerIdentity(vm, runnerName, runnerID); err != nil {
		allow, reason, fallbackErr := allowDeleteAfterIdentityMismatch(ctx, cfg, client, vm, runnerName, runnerID)
		if !allow {
			if fallbackErr != nil {
				return xerrors.Errorf("refusing delete after identity mismatch: %w (fallback failed: %v)", err, fallbackErr)
			}
			return err
		}
		log.Warnw("Proceeding with delete after identity mismatch via fallback verification",
			"runnerName", runnerName,
			"runnerID", runnerID,
			"reason", reason,
			"identityError", err.Error())
	}

	provider, err := cfg.provider()
	if err != nil {
		return err
	}
	_, err = callWithRetry(ctx, cfg, "DeleteRunnerVM", cfg.Runtime.APIWriteAttempts, func(callCtx context.Context) (struct{}, error) {
		return struct{}{}, provider.DeleteRunnerVM(callCtx, vm)
	})
	return err
}

func (cfg *Config) getRunnerVMByName(ctx context.Context, runnerName string) (ManagedRunnerVM, error) {
	vms, err := cfg.listManagedRunnerVMs(ctx)
	if err != nil {
		return ManagedRunnerVM{}, err
	}

	var found *ManagedRunnerVM
	for i := range vms {
		vm := &vms[i]
		if vm.Name != runnerName {
			continue
		}
		if found != nil {
			return ManagedRunnerVM{}, xerrors.Errorf("runner VM %q found in multiple placements (%s, %s)", runnerName, found.Zone, vm.Zone)
		}
		found = vm
	}

	if found == nil {
		return ManagedRunnerVM{}, errRunnerVMNotFound
	}
	return *found, nil
}

func isZoneCapacityError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "ZONE_RESOURCE_POOL_EXHAUSTED") ||
		strings.Contains(msg, "ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS") ||
		strings.Contains(msg, "RESOURCE_AVAILABILITY") ||
		strings.Contains(msg, "CPU_AVAILABILITY") ||
		strings.Contains(msg, "MEMORY_AVAILABILITY")
}

func allowDeleteAfterIdentityMismatch(ctx context.Context, cfg *Config, client scaleSetAPI, vm ManagedRunnerVM, expectedName string, expectedID int) (bool, string, error) {
	if vm.Name != expectedName {
		return false, "", nil
	}

	// Preferred fallback: validate against scaleset runner identity.
	if client != nil {
		if ok, err := validateRunnerViaScaleSet(ctx, cfg, client, expectedName, expectedID); ok {
			return true, "validated_via_scaleset_runner_reference", nil
		} else if err != nil {
			log.Warnw("Scale set identity fallback failed", "runnerName", expectedName, "runnerID", expectedID, "error", err.Error())
		}
	}

	// Last fallback: exact name match + managed runner marker metadata.
	if vm.HasJITConfig {
		return true, "validated_via_instance_name_and_jit_marker", nil
	}

	return false, "", nil
}

func validateRunnerViaScaleSet(ctx context.Context, cfg *Config, client scaleSetAPI, expectedName string, expectedID int) (bool, error) {
	runnerByID, errByID := callWithRetry(ctx, cfg, "GetRunnerByID", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerReference, error) {
		return client.GetRunner(callCtx, expectedID)
	})
	if errByID == nil && runnerByID != nil {
		if runnerByID.Name != expectedName {
			return false, xerrors.Errorf("runner name mismatch via ID lookup: got %q expected %q", runnerByID.Name, expectedName)
		}
		return true, nil
	}

	runnerByName, errByName := callWithRetry(ctx, cfg, "GetRunnerByName", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerReference, error) {
		return client.GetRunnerByName(callCtx, expectedName)
	})
	if errByName == nil && runnerByName != nil {
		if runnerByName.ID != expectedID {
			return false, xerrors.Errorf("runner ID mismatch via name lookup: got %d expected %d", runnerByName.ID, expectedID)
		}
		return true, nil
	}

	return false, xerrors.Errorf("runner lookups failed: by-id err=%v by-name err=%v", errByID, errByName)
}

func isRunnerMissingFromScaleSet(ctx context.Context, cfg *Config, client scaleSetAPI, runnerName string, runnerID int) (bool, error) {
	if client == nil {
		return false, xerrors.New("scaleset client is nil")
	}

	runnerByName, err := callWithRetry(ctx, cfg, "GetRunnerByName", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerReference, error) {
		return client.GetRunnerByName(callCtx, runnerName)
	})
	if err != nil {
		if isNotFoundScaleSetErr(err) {
			return true, nil
		}
		return false, err
	}
	if runnerByName == nil {
		return true, nil
	}
	if runnerByName.ID != runnerID {
		return true, nil
	}
	return false, nil
}

func isNotFoundScaleSetErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "404")
}

func runnerVMAge(vm ManagedRunnerVM, now time.Time) time.Duration {
	if !vm.HasCreationTime {
		return 0
	}
	age := now.Sub(vm.CreatedAt)
	if age < 0 {
		return 0
	}
	return age
}

func validateRunnerIdentity(vm ManagedRunnerVM, expectedName string, expectedID int) error {
	expectedIDStr := strconv.Itoa(expectedID)
	gotName := vm.RunnerName
	gotID := ""
	if vm.HasIdentity {
		gotID = strconv.Itoa(vm.RunnerID)
	}

	if gotName != expectedName || gotID != expectedIDStr {
		return xerrors.Errorf("refusing delete: identity mismatch runner-name=%q runner-id=%q expected-name=%q expected-id=%q",
			gotName, gotID, expectedName, expectedIDStr)
	}
	return nil
}
