# GCP Provider

`packages/providers/gcp` contains the Google Cloud provider implementation for `scalefleet`.

This package owns all GCP-specific behavior:
- Compute Engine VM list/create/delete calls
- Zone fallback on capacity errors
- Secret Manager reads for auth secrets
- GCP provider config validation

Core scaler logic stays cloud-agnostic and calls this provider through interfaces.

## Core vs Provider Boundary

Core (`packages/scaler`) owns:

- GitHub scaleset client/session lifecycle
- scaleset create/reuse flow
- listener-driven reconciliation and intent engine
- retry/backoff policy
- orphan sweep policy and identity verification logic

GCP provider (`packages/providers/gcp`) owns:

- GCP VM list/create/delete API calls
- GCP zone placement/fallback for VM creation
- VM metadata/resource mapping details
- Secret Manager read implementation
- GCP-specific config validation

## Config

Use `*gcp.Config` as `scaler.Config.Provider`.

```go
cfg := scaler.DefaultConfig()

providerCfg := gcp.DefaultConfig()
providerCfg.ProjectID = "YOUR_GCP_PROJECT_ID"
providerCfg.Zones = []string{
	"us-central1-a",
	"us-central1-b",
	"us-central1-c",
	"us-central1-f",
}
providerCfg.Runner.Image = "projects/YOUR_GCP_PROJECT_ID/global/images/YOUR_RUNNER_IMAGE"
providerCfg.Runner.MachineType = "e2-standard-8"
providerCfg.Runner.Network = "global/networks/default"
providerCfg.Runner.ServiceAccountEmail = "runner@YOUR_GCP_PROJECT_ID.iam.gserviceaccount.com"
providerCfg.Runner.NetworkTags = []string{"iap-ssh"}

cfg.Provider = providerCfg
```

Required fields:
- `ProjectID`
- `Zones` (non-empty, no blank values)
- `Runner.Image`
- `Runner.MachineType`
- `Runner.Network`
- `Runner.ServiceAccountEmail`
- `Runner.RunnerMaxRunDuration` (set by `gcp.DefaultConfig()`)

## Provider Behavior

### List
- Lists instances by managed prefix across configured zones.
- Only active VM statuses are considered: `PROVISIONING`, `STAGING`, `RUNNING`.

### Create
- Creates runner VM in the first zone that succeeds.
- Falls back to next zone only for capacity-style errors:
  - `ZONE_RESOURCE_POOL_EXHAUSTED`
  - `ZONE_RESOURCE_POOL_EXHAUSTED_WITH_DETAILS`
  - `RESOURCE_AVAILABILITY`
  - `CPU_AVAILABILITY`
  - `MEMORY_AVAILABILITY`
- VM create constraints:
  - No external IP (`AccessConfigs: []`)
  - Metadata keys include `jit-config`, `runner-name`, `runner-id`, `runner-machine-type`, `runner-zone`, `startup-script`
  - Scheduling uses `instanceTerminationAction=DELETE` and provider-derived `maxRunDuration`

### Delete
- Deletes by resolved VM `name + zone`.

### Secret Loading
- Reads from:
  - `projects/<PROJECT_ID>/secrets/<SECRET_NAME>/versions/latest`
- Returns trimmed secret payload value.

## Provider Authoring Reference

This package is the reference implementation for building new providers.

To add another provider, mirror this structure:

1. Add `packages/providers/<provider>/config.go` with provider config + validation.
2. Add `packages/providers/<provider>/provider.go` implementing `scaler.CloudProvider`.
3. Implement all required contract methods:
   - `MachineType()`
   - `Validate()`
   - `RunnerMaxRunDuration()`
   - `ListManagedRunnerVMs(...)`
   - `CreateRunnerVM(...)`
   - `DeleteRunnerVM(...)`
   - `LoadSecretValue(...)`
4. Add provider-focused tests.
5. Add provider README with setup/prerequisites and CLI usage specifics.

## VM Specification Customization

This provider exposes VM behavior through `gcp.Config.Runner` fields:

- `MachineType`
- `Image`
- `Network`
- `ServiceAccountEmail`
- `NetworkTags`
- `Script`
- `RunnerMaxRunDuration`

Easy extension path for richer user-controlled VM specs:

1. Add optional fields to `RunnerConfig` (for example disk size/type, labels, scopes, extra metadata).
2. Set defaults in `gcp.DefaultConfig()`.
3. Map fields in `buildRunnerInstance(...)`.
4. Validate field invariants in `(*Config).Validate()`.

## GCP Prerequisites

## APIs
- Enable `Compute Engine API`.
- Enable `Secret Manager API`.

## Service Accounts
- Controller identity (the one running scalefleet).
- Runner VM service account (attached to created runner VMs).

## IAM for Controller Identity
- `roles/compute.instanceAdmin.v1` (or equivalent minimal custom permissions for VM list/get/create/delete).
- `roles/secretmanager.secretAccessor` for required secrets.
- `roles/iam.serviceAccountUser` on the runner VM service account.

## Networking
- Runner VMs are created without external IP.
- Provide private egress (for example Cloud NAT) so runners can reach GitHub.
- Ensure outbound HTTPS (`443`) to required GitHub domains.
- If using network tags, create matching firewall rules.

## Secret Manager

Store GitHub auth values in Secret Manager and pass only secret names via config.

PAT mode:
- PAT secret name

GitHub App mode:
- App client ID secret name
- App installation ID secret name
- App private key secret name

## Runner Image

`Runner.Image` must point to a valid GCE image path accessible in `ProjectID`.

The default startup script expects these tools in the image:
- `curl`
- `install`
- `tar`
- `awk`
- `runuser`
- `useradd`
- `uname`
- `tr`

## Notes
- Library callers must set `Zones` explicitly.
- This provider README is GCP-only; keep cloud-agnostic behavior documentation in core package docs.
