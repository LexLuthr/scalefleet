package gcp

import (
	"testing"

	"github.com/LexLuthr/scalefleet/packages/scaler"
)

func TestBuildRunnerInstanceEnforcesNoExternalIPAndNetworkTag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProjectID = "YOUR_GCP_PROJECT_ID"
	cfg.Runner.MachineType = "e2-standard-8"
	cfg.Runner.Image = "projects/YOUR_GCP_PROJECT_ID/global/images/runner-image-v1"
	cfg.Runner.Network = "global/networks/default"
	cfg.Runner.ServiceAccountEmail = "runner@YOUR_GCP_PROJECT_ID.iam.gserviceaccount.com"
	cfg.Runner.NetworkTags = []string{"iap-ssh"}

	instance := cfg.buildRunnerInstance("us-central1-a", scaler.CreateRunnerVMRequest{
		JITConfig:             "encoded-jit",
		RunnerName:            "scalefleet-ci-runner-1",
		RunnerID:              77,
		MaxRunDurationSeconds: 1800,
	})

	if instance.GetMachineType() != "projects/YOUR_GCP_PROJECT_ID/zones/us-central1-a/machineTypes/e2-standard-8" {
		t.Fatalf("unexpected machine type: %q", instance.GetMachineType())
	}
	if len(instance.GetNetworkInterfaces()) != 1 {
		t.Fatalf("unexpected network interface count: %d", len(instance.GetNetworkInterfaces()))
	}
	nic := instance.GetNetworkInterfaces()[0]
	if nic.GetNetwork() != "global/networks/default" {
		t.Fatalf("unexpected network: %q", nic.GetNetwork())
	}
	if len(nic.GetAccessConfigs()) != 0 {
		t.Fatalf("expected no external access configs, got: %d", len(nic.GetAccessConfigs()))
	}
	if instance.GetTags() == nil {
		t.Fatal("expected tags to be set")
	}
	if len(instance.GetTags().GetItems()) != 1 || instance.GetTags().GetItems()[0] != "iap-ssh" {
		t.Fatalf("unexpected tags: %#v", instance.GetTags().GetItems())
	}
	if instance.GetScheduling() == nil || instance.GetScheduling().GetMaxRunDuration() == nil {
		t.Fatal("expected scheduling max run duration to be set")
	}
	if instance.GetScheduling().GetMaxRunDuration().GetSeconds() != 1800 {
		t.Fatalf("unexpected max run duration seconds: %d", instance.GetScheduling().GetMaxRunDuration().GetSeconds())
	}
}

func TestBuildRunnerInstanceSkipsNetworkTagWhenEmpty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ProjectID = "YOUR_GCP_PROJECT_ID"
	cfg.Runner.MachineType = "e2-standard-8"
	cfg.Runner.Image = "projects/YOUR_GCP_PROJECT_ID/global/images/runner-image-v1"
	cfg.Runner.Network = "global/networks/default"
	cfg.Runner.ServiceAccountEmail = "runner@YOUR_GCP_PROJECT_ID.iam.gserviceaccount.com"
	cfg.Runner.NetworkTags = []string{"   "}

	instance := cfg.buildRunnerInstance("us-central1-a", scaler.CreateRunnerVMRequest{
		JITConfig:             "encoded-jit",
		RunnerName:            "scalefleet-ci-runner-2",
		RunnerID:              88,
		MaxRunDurationSeconds: 1800,
	})
	if instance.GetTags() != nil {
		t.Fatalf("expected nil tags when runner network tag is empty, got: %#v", instance.GetTags().GetItems())
	}
}
