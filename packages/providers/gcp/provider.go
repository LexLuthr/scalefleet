package gcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/LexLuthr/scalefleet/packages/scaler"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
	"google.golang.org/api/iterator"
	"google.golang.org/api/secretmanager/v1"
)

var log = logging.Logger("scalefleet-provider-gcp")

// ListManagedRunnerVMs lists active managed runner VMs for the supplied managed prefix.
func (cfg *Config) ListManagedRunnerVMs(ctx context.Context, managedPrefix string) ([]scaler.ManagedRunnerVM, error) {
	instances := make([]scaler.ManagedRunnerVM, 0)
	for _, zone := range cfg.Zones {
		zoneInstances, err := cfg.listManagedRunnerVMsInZone(ctx, zone, managedPrefix)
		if err != nil {
			return instances, err
		}
		instances = append(instances, zoneInstances...)
	}
	return instances, nil
}

func (cfg *Config) listManagedRunnerVMsInZone(ctx context.Context, zone, managedPrefix string) ([]scaler.ManagedRunnerVM, error) {
	c, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = c.Close()
	}()

	filter := fmt.Sprintf(`name eq "%s-.*"`, managedPrefix)
	req := &computepb.ListInstancesRequest{
		Project: cfg.ProjectID,
		Zone:    zone,
		Filter:  &filter,
	}
	it := c.List(ctx, req)

	instances := make([]scaler.ManagedRunnerVM, 0)
	for {
		instance, iterErr := it.Next()
		if errors.Is(iterErr, iterator.Done) {
			break
		}
		if iterErr != nil {
			return instances, iterErr
		}
		if instance == nil {
			continue
		}
		if !isActiveRunnerVMStatus(instance.GetStatus()) {
			continue
		}
		instances = append(instances, mapInstance(instance, zone))
	}
	return instances, nil
}

// CreateRunnerVM creates a managed runner VM with zone fallback on capacity errors.
func (cfg *Config) CreateRunnerVM(ctx context.Context, req scaler.CreateRunnerVMRequest) (string, error) {
	var lastCapacityErr error
	for _, zone := range cfg.Zones {
		err := cfg.createRunnerVMInZone(ctx, zone, req)
		if err == nil {
			return zone, nil
		}
		if !isZoneCapacityError(err) {
			return "", err
		}
		lastCapacityErr = err
		log.Warnw("Runner VM create hit zone capacity, trying next zone",
			"runnerName", req.RunnerName,
			"runnerID", req.RunnerID,
			"zone", zone,
			"error", err.Error())
	}
	if lastCapacityErr != nil {
		return "", xerrors.Errorf("create runner VM failed in all configured zones due to capacity: %w", lastCapacityErr)
	}
	return "", xerrors.New("runner zone configuration is empty")
}

func (cfg *Config) createRunnerVMInZone(ctx context.Context, zone string, req scaler.CreateRunnerVMRequest) error {
	restClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = restClient.Close()
	}()

	insertReq := &computepb.InsertInstanceRequest{
		Project:          cfg.ProjectID,
		Zone:             zone,
		InstanceResource: cfg.buildRunnerInstance(zone, req),
	}

	_, err = restClient.Insert(ctx, insertReq)
	return err
}

func (cfg *Config) buildRunnerInstance(zone string, req scaler.CreateRunnerVMRequest) *computepb.Instance {
	instance := &computepb.Instance{
		Name:        strPtr(req.RunnerName),
		Hostname:    strPtr(req.RunnerName),
		MachineType: strPtr(fmt.Sprintf("projects/%s/zones/%s/machineTypes/%s", cfg.ProjectID, zone, cfg.MachineType())),
		Disks: []*computepb.AttachedDisk{
			{
				AutoDelete: boolPtr(true),
				Boot:       boolPtr(true),
				Type:       strPtr("PERSISTENT"),
				InitializeParams: &computepb.AttachedDiskInitializeParams{
					SourceImage: strPtr(cfg.Runner.Image),
					DiskSizeGb:  int64Ptr(50),
					DiskType:    strPtr(fmt.Sprintf("projects/%s/zones/%s/diskTypes/pd-ssd", cfg.ProjectID, zone)),
				},
				Mode: strPtr("READ_WRITE"),
			},
		},
		NetworkInterfaces: []*computepb.NetworkInterface{
			{
				StackType:     strPtr("IPV4_ONLY"),
				Network:       strPtr(cfg.Runner.Network),
				AccessConfigs: []*computepb.AccessConfig{},
			},
		},
		ServiceAccounts: []*computepb.ServiceAccount{
			{
				Email:  strPtr(cfg.Runner.ServiceAccountEmail),
				Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
			},
		},
		Metadata: &computepb.Metadata{
			Items: []*computepb.Items{
				{Key: strPtr("jit-config"), Value: strPtr(req.JITConfig)},
				{Key: strPtr("runner-name"), Value: strPtr(req.RunnerName)},
				{Key: strPtr("runner-id"), Value: strPtr(strconv.Itoa(req.RunnerID))},
				{Key: strPtr("runner-machine-type"), Value: strPtr(cfg.MachineType())},
				{Key: strPtr("runner-zone"), Value: strPtr(zone)},
				{Key: strPtr("startup-script"), Value: strPtr(cfg.Runner.Script)},
			},
		},
		Scheduling: &computepb.Scheduling{
			ProvisioningModel:         strPtr("STANDARD"),
			InstanceTerminationAction: strPtr("DELETE"),
			MaxRunDuration: &computepb.Duration{
				Seconds: int64Ptr(req.MaxRunDurationSeconds),
			},
			AutomaticRestart:    boolPtr(false),
			OnHostMaintenance:   strPtr("TERMINATE"),
			SkipGuestOsShutdown: boolPtr(true),
		},
	}

	tags := make([]string, 0, len(cfg.Runner.NetworkTags))
	for _, tag := range cfg.Runner.NetworkTags {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	if len(tags) > 0 {
		instance.Tags = &computepb.Tags{Items: tags}
	}
	return instance
}

// DeleteRunnerVM deletes a managed runner VM by resolved provider placement.
func (cfg *Config) DeleteRunnerVM(ctx context.Context, vm scaler.ManagedRunnerVM) error {
	if strings.TrimSpace(vm.Zone) == "" {
		return xerrors.New("runner VM zone is required for delete")
	}
	if strings.TrimSpace(vm.Name) == "" {
		return xerrors.New("runner VM name is required for delete")
	}

	restClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = restClient.Close()
	}()

	_, err = restClient.Delete(ctx, &computepb.DeleteInstanceRequest{
		Project:  cfg.ProjectID,
		Zone:     vm.Zone,
		Instance: vm.Name,
	})
	return err
}

// LoadSecretValue loads and decodes a secret from GCP Secret Manager.
func (cfg *Config) LoadSecretValue(ctx context.Context, secretName string) (string, error) {
	svc, err := secretmanager.NewService(ctx)
	if err != nil {
		return "", xerrors.Errorf("create secret manager service: %w", err)
	}

	secretPath := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", cfg.ProjectID, secretName)
	resp, err := svc.Projects.Secrets.Versions.Access(secretPath).Context(ctx).Do()
	if err != nil {
		return "", xerrors.Errorf("access secret version: %w", err)
	}
	if resp.Payload == nil || resp.Payload.Data == "" {
		return "", xerrors.New("secret payload is empty")
	}

	raw, err := base64.StdEncoding.DecodeString(resp.Payload.Data)
	if err != nil {
		return "", xerrors.Errorf("decode secret payload: %w", err)
	}

	return strings.TrimSpace(string(raw)), nil
}

func isActiveRunnerVMStatus(status string) bool {
	switch status {
	case "PROVISIONING", "STAGING", "RUNNING":
		return true
	default:
		return false
	}
}

func mapInstance(instance *computepb.Instance, zone string) scaler.ManagedRunnerVM {
	vm := scaler.ManagedRunnerVM{
		Name: instance.GetName(),
		Zone: zone,
	}

	runnerName, hasRunnerName := metadataValue(instance, "runner-name")
	runnerIDRaw, hasRunnerID := metadataValue(instance, "runner-id")
	if hasRunnerName && hasRunnerID {
		if runnerID, err := strconv.Atoi(strings.TrimSpace(runnerIDRaw)); err == nil {
			vm.RunnerName = strings.TrimSpace(runnerName)
			vm.RunnerID = runnerID
			vm.HasIdentity = true
		}
	}

	_, vm.HasJITConfig = metadataValue(instance, "jit-config")

	createdAt, ok := parseCreationTime(instance.GetCreationTimestamp())
	if ok {
		vm.CreatedAt = createdAt
		vm.HasCreationTime = true
	}

	return vm
}

func metadataValue(instance *computepb.Instance, key string) (string, bool) {
	if instance == nil || instance.Metadata == nil {
		return "", false
	}
	for _, item := range instance.Metadata.Items {
		if item == nil || item.Key == nil || item.Value == nil {
			continue
		}
		if *item.Key != key {
			continue
		}
		value := strings.TrimSpace(*item.Value)
		if value == "" {
			return "", false
		}
		return value, true
	}
	return "", false
}

func parseCreationTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	createdAt, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return createdAt, true
	}

	createdAt, err = time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return createdAt, true
	}
	return time.Time{}, false
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

func strPtr(v string) *string {
	ret := v
	return &ret
}

func boolPtr(v bool) *bool {
	ret := v
	return &ret
}

func int64Ptr(v int64) *int64 {
	ret := v
	return &ret
}
