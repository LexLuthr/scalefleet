package main

import (
	"flag"
	"strings"
	"testing"

	"github.com/LexLuthr/scalefleet/packages/providers/gcp"
	"github.com/urfave/cli/v2"
)

func newRunGCPConfigContext(t *testing.T, args []string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("run-gcp", flag.ContinueOnError)
	for _, f := range runGCPConfigFlags() {
		if err := f.Apply(set); err != nil {
			t.Fatalf("failed to apply flag %q: %v", f.Names()[0], err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatalf("failed to parse args: %v", err)
	}
	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestApplyRunGCPConfigFromFlags(t *testing.T) {
	ctx := newRunGCPConfigContext(t, []string{
		"--project-id=test-project",
		"--runner-image=projects/test-project/global/images/runner-image-v1",
		"--runner-machine-type=e2-standard-16",
		"--runner-network=global/networks/default",
		"--runner-sa-email=runner@test-project.iam.gserviceaccount.com",
		"--runner-network-tag=iap-ssh",
		"--pat-secret-name=gh-pat-secret",
	})

	cfg := applyRunGCPConfigFromFlags(ctx)
	err := cfg.RunTimeConfig()
	if err != nil {
		t.Fatalf("applyRunGCPConfigFromFlags returned error: %v", err)
	}
	providerCfg, ok := cfg.Provider.(*gcp.Config)
	if !ok {
		t.Fatalf("unexpected provider type: %T", cfg.Provider)
	}

	if providerCfg.ProjectID != "test-project" {
		t.Fatalf("unexpected projectID: %q", providerCfg.ProjectID)
	}
	if providerCfg.Runner.Image != "projects/test-project/global/images/runner-image-v1" {
		t.Fatalf("unexpected runnerImage: %q", providerCfg.Runner.Image)
	}
	if providerCfg.Runner.MachineType != "e2-standard-16" {
		t.Fatalf("unexpected runnerMachineType: %q", providerCfg.Runner.MachineType)
	}
	if providerCfg.Runner.Network != "global/networks/default" {
		t.Fatalf("unexpected runnerNetwork: %q", providerCfg.Runner.Network)
	}
	if providerCfg.Runner.ServiceAccountEmail != "runner@test-project.iam.gserviceaccount.com" {
		t.Fatalf("unexpected runnerSAEmail: %q", providerCfg.Runner.ServiceAccountEmail)
	}
	if providerCfg.Runner.NetworkTags[0] != "iap-ssh" {
		t.Fatalf("unexpected runnerNetworkTag: %q", providerCfg.Runner.NetworkTags)
	}
	if cfg.GitHub.ConfigURL != "https://github.com/LexLuthr/scalefleet" {
		t.Fatalf("unexpected githubConfigURL: %q", cfg.GitHub.ConfigURL)
	}
	if !cfg.GitHub.Auth.PATMode {
		t.Fatal("expected PAT auth mode to be true")
	}
	if cfg.GitHub.Auth.PATSecretName != "gh-pat-secret" {
		t.Fatalf("unexpected patSecretName: %q", cfg.GitHub.Auth.PATSecretName)
	}
}

func TestApplyRunGCPConfigFromFlagsAcceptsCustomRunnerMachineType(t *testing.T) {
	ctx := newRunGCPConfigContext(t, []string{
		"--project-id=test-project",
		"--runner-image=projects/test-project/global/images/runner-image-v1",
		"--runner-machine-type=n2-standard-8",
		"--runner-network=global/networks/default",
		"--runner-sa-email=runner@test-project.iam.gserviceaccount.com",
		"--github-config-url=https://github.com/acme/ci",
		"--pat-secret-name=gh-pat-secret",
	})

	cfg := applyRunGCPConfigFromFlags(ctx)
	err := cfg.RunTimeConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	providerCfg, ok := cfg.Provider.(*gcp.Config)
	if !ok {
		t.Fatalf("unexpected provider type: %T", cfg.Provider)
	}
	if providerCfg.Runner.MachineType != "n2-standard-8" {
		t.Fatalf("unexpected runnerMachineType: %q", providerCfg.Runner.MachineType)
	}
	if cfg.GitHub.ConfigURL != "https://github.com/acme/ci" {
		t.Fatalf("unexpected githubConfigURL: %q", cfg.GitHub.ConfigURL)
	}
}

func TestApplyRunGCPConfigFromFlagsRequiresPATSecretWhenPATModeEnabled(t *testing.T) {
	ctx := newRunGCPConfigContext(t, []string{
		"--project-id=test-project",
		"--runner-image=projects/test-project/global/images/runner-image-v1",
		"--runner-network=global/networks/default",
		"--runner-sa-email=runner@test-project.iam.gserviceaccount.com",
	})

	cfg := applyRunGCPConfigFromFlags(ctx)
	err := cfg.RunTimeConfig()
	if err == nil {
		t.Fatal("expected error when PAT secret name is omitted")
	}
	if !strings.Contains(err.Error(), "pat-secret-name is required when github-auth-pat-mode is true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyRunGCPConfigFromFlagsRequiresGitHubAppSecretsWhenPATModeDisabled(t *testing.T) {
	ctx := newRunGCPConfigContext(t, []string{
		"--project-id=test-project",
		"--runner-image=projects/test-project/global/images/runner-image-v1",
		"--runner-network=global/networks/default",
		"--runner-sa-email=runner@test-project.iam.gserviceaccount.com",
		"--github-auth-pat-mode=false",
	})

	cfg := applyRunGCPConfigFromFlags(ctx)
	err := cfg.RunTimeConfig()
	if err == nil {
		t.Fatal("expected error when GitHub App secrets are omitted")
	}
	if !strings.Contains(err.Error(), "github-app-client-id-secret-name is required when github-auth-pat-mode is false") {
		t.Fatalf("unexpected error: %v", err)
	}
}
