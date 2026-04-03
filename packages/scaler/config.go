package scaler

import (
	"fmt"
	"strings"
	"time"

	"github.com/actions/scaleset"
	"golang.org/x/xerrors"
)

// Config is the single top-level configuration used to run the scalefleet library.
type Config struct {
	// Build defines system identity metadata sent to GitHub scale set APIs.
	Build BuildConfig
	// GitHub defines API target and authentication configuration.
	GitHub GitHubConfig
	// Provider is the single active cloud provider config for this run.
	Provider CloudProvider
	// ScaleSet defines naming prefixes and listener limits for scale set resources.
	ScaleSet ScaleSetConfig
	// Runtime defines API retry, worker, and lifecycle timing behavior.
	// It also carries internal derived fields populated by initRuntimeVars.
	Runtime RuntimeConfig
}

// BuildConfig defines controller identity values advertised to GitHub APIs.
type BuildConfig struct {
	// System is the logical system name reported by the controller.
	System string
	// Version is the controller version string reported by the controller.
	Version string
	// CommitSHA is the source revision string reported by the controller.
	CommitSHA string
}

// GitHubConfig defines GitHub endpoint and auth secret behavior.
type GitHubConfig struct {
	// ConfigURL is the GitHub configuration URL used by the scale set client.
	// This can be a repo URL or org URL. If an org URL is used, then runner groups
	// must be created manually.
	ConfigURL string
	// WorkFolder is the working directory hint sent in JIT config requests.
	WorkFolder string
	// Auth defines how GitHub credentials are loaded from Secret Manager.
	Auth GitHubAuthConfig
	// RunnerGroupName is the GitHub runner group looked up before scale set operations.
	RunnerGroupName string
}

// GitHubAuthConfig defines GitHub auth mode and required secret names.
type GitHubAuthConfig struct {
	// PATMode enables PAT auth when true and GitHub App auth when false.
	PATMode bool
	// PATSecretName is the secret name containing a GitHub PAT.
	PATSecretName string
	// AppClientIDSecretName is the secret name containing the GitHub App client ID.
	AppClientIDSecretName string
	// AppInstallationIDSecretName is the secret name containing the GitHub App installation ID.
	AppInstallationIDSecretName string
	// AppPrivateKeySecretName is the secret name containing the GitHub App private key.
	AppPrivateKeySecretName string
}

// ScaleSetConfig defines naming prefixes for resources managed by this controller.
type ScaleSetConfig struct {
	// NamePrefix is the prefix used for runner scale set names.
	// The full name is derived as <NamePrefix>-<lower(MachineType)>.
	NamePrefix string
	// VMPrefix is the prefix used for managed runner VM names.
	// The full VM prefix is derived as <VMPrefix>-<lower(MachineType)>.
	VMPrefix string
	// MaxRunners is the maximum listener-reported runner capacity.
	MaxRunners int
}

// RuntimeConfig defines retry and lifecycle timing values.
type RuntimeConfig struct {
	// APICallTimeout is the timeout for a single API attempt.
	APICallTimeout time.Duration
	// APIReadAttempts is the retry attempt count for read operations.
	APIReadAttempts int
	// APIWriteAttempts is the retry attempt count for write operations.
	APIWriteAttempts int
	// APIInitialBackoff is the initial exponential backoff used by retries.
	APIInitialBackoff time.Duration
	// OrphanRunnerGracePeriod is the minimum VM age before orphan cleanup checks.
	OrphanRunnerGracePeriod time.Duration
	// OrphanRunnerSweepInterval is the periodic interval for orphan sweeps.
	OrphanRunnerSweepInterval time.Duration
	// Worker defines asynchronous worker behavior for create/delete intents.
	Worker WorkerConfig
	// scaleSetName is the internal scale set name derived from ScaleSet.NamePrefix and runner machine type.
	scaleSetName string
	// runnerVMPrefix is the internal managed VM name prefix derived from ScaleSet.VMPrefix and runner machine type.
	runnerVMPrefix string
	// runnerMaxRunDurationSeconds is the internal max run duration derived from provider config.
	runnerMaxRunDurationSeconds int64
}

// WorkerConfig defines asynchronous intent engine behavior.
type WorkerConfig struct {
	// WorkerCount is the number of worker goroutines processing intents.
	WorkerCount int
	// ConfirmInterval is the interval for submitted intent confirmation checks.
	ConfirmInterval time.Duration
	// UnknownStateTimeout is the timeout for submitted intents stuck in unknown state.
	UnknownStateTimeout time.Duration
	// CapacityRetryCooldown is the retry cooldown after zone capacity failures.
	CapacityRetryCooldown time.Duration
	// RetryBackoff is the default retry delay for non-capacity failures.
	RetryBackoff time.Duration
}

// DefaultConfig returns a baseline config initialized from package constants.
// Environment-specific values (for example project, image, network, service account,
// zones, provider config, and auth secret names) are expected to be supplied by the caller.
func DefaultConfig() Config {
	return Config{
		Build: BuildConfig{
			System:    System,
			Version:   Version,
			CommitSHA: CommitSHA,
		},
		GitHub: GitHubConfig{
			ConfigURL:  githubConfigURL,
			WorkFolder: workFolder,
			Auth: GitHubAuthConfig{
				PATMode: true,
			},
			RunnerGroupName: scaleset.DefaultRunnerGroup,
		},
		ScaleSet: ScaleSetConfig{
			NamePrefix: scaleSetNamePrefix,
			VMPrefix:   vmPrefix,
			MaxRunners: 100,
		},
		Runtime: RuntimeConfig{
			APICallTimeout:            apiCallTimeout,
			APIReadAttempts:           apiReadAttempts,
			APIWriteAttempts:          apiWriteAttempts,
			APIInitialBackoff:         apiInitialBackoff,
			OrphanRunnerGracePeriod:   orphanRunnerGracePeriod,
			OrphanRunnerSweepInterval: orphanRunnerSweepInterval,
			Worker: WorkerConfig{
				WorkerCount:           intentWorkerCount,
				ConfirmInterval:       intentConfirmInterval,
				UnknownStateTimeout:   intentUnknownStateTimeout,
				CapacityRetryCooldown: intentCapacityRetryCooldown,
				RetryBackoff:          intentRetryBackoff,
			},
		},
	}
}

// validate verifies core and provider-specific config invariants before runtime starts.
func (cfg *Config) validate() error {
	if cfg == nil {
		return xerrors.New("config is nil")
	}
	if cfg.Provider == nil {
		return xerrors.New("provider is required")
	}
	if strings.TrimSpace(cfg.Provider.MachineType()) == "" {
		return xerrors.New("provider.machineType is required")
	}
	if err := cfg.Provider.Validate(); err != nil {
		return xerrors.Errorf("invalid provider configuration: %w", err)
	}

	if cfg.Runtime.APIReadAttempts < 1 {
		return xerrors.New("runtime.apiReadAttempts must be >= 1")
	}
	if cfg.Runtime.APIWriteAttempts < 1 {
		return xerrors.New("runtime.apiWriteAttempts must be >= 1")
	}
	if cfg.Runtime.APICallTimeout <= 0 {
		return xerrors.New("runtime.apiCallTimeout must be > 0")
	}
	if cfg.Runtime.APIInitialBackoff <= 0 {
		return xerrors.New("runtime.apiInitialBackoff must be > 0")
	}
	if cfg.Runtime.OrphanRunnerGracePeriod <= 0 {
		return xerrors.New("runtime.orphanRunnerGracePeriod must be > 0")
	}
	if cfg.Runtime.OrphanRunnerSweepInterval <= 0 {
		return xerrors.New("runtime.orphanRunnerSweepInterval must be > 0")
	}
	if cfg.Runtime.Worker.WorkerCount < 1 {
		return xerrors.New("runtime.worker.workerCount must be >= 1")
	}
	if cfg.Runtime.Worker.ConfirmInterval <= 0 {
		return xerrors.New("runtime.worker.confirmInterval must be > 0")
	}
	if cfg.Runtime.Worker.UnknownStateTimeout <= 0 {
		return xerrors.New("runtime.worker.unknownStateTimeout must be > 0")
	}
	if cfg.Runtime.Worker.CapacityRetryCooldown <= 0 {
		return xerrors.New("runtime.worker.capacityRetryCooldown must be > 0")
	}
	if cfg.Runtime.Worker.RetryBackoff <= 0 {
		return xerrors.New("runtime.worker.retryBackoff must be > 0")
	}

	if cfg.GitHub.Auth.PATMode {
		if strings.TrimSpace(cfg.GitHub.Auth.PATSecretName) == "" {
			return xerrors.New("pat-secret-name is required when github-auth-pat-mode is true")
		}
	} else {
		if strings.TrimSpace(cfg.GitHub.Auth.AppClientIDSecretName) == "" {
			return xerrors.New("github-app-client-id-secret-name is required when github-auth-pat-mode is false")
		}
		if strings.TrimSpace(cfg.GitHub.Auth.AppInstallationIDSecretName) == "" {
			return xerrors.New("github-app-installation-id-secret-name is required when github-auth-pat-mode is false")
		}
		if strings.TrimSpace(cfg.GitHub.Auth.AppPrivateKeySecretName) == "" {
			return xerrors.New("github-app-private-key-secret-name is required when github-auth-pat-mode is false")
		}
	}

	return nil
}

// initRuntimeVars derives internal runtime fields from user-provided config values.
// It must run after validate succeeds.
func initRuntimeVars(cfg *Config) error {
	if cfg == nil {
		return xerrors.New("config is nil")
	}
	machineType := cfg.Provider.MachineType()
	cfg.Runtime.scaleSetName = fmt.Sprintf("%s-%s", strings.TrimSpace(cfg.ScaleSet.NamePrefix), machineType)
	cfg.Runtime.runnerVMPrefix = fmt.Sprintf("%s-%s", strings.TrimSpace(cfg.ScaleSet.VMPrefix), machineType)
	runnerMaxRunDuration := cfg.Provider.RunnerMaxRunDuration()
	if runnerMaxRunDuration <= 0 {
		return xerrors.New("provider runner max run duration must be > 0")
	}
	cfg.Runtime.runnerMaxRunDurationSeconds = int64(runnerMaxRunDuration.Seconds())
	if cfg.Runtime.runnerMaxRunDurationSeconds <= 0 {
		return xerrors.New("provider runner max run duration seconds must be > 0")
	}
	return nil
}

// RunTimeConfig validates config and derives internal runtime-only fields.
func (cfg *Config) RunTimeConfig() error {
	if err := cfg.validate(); err != nil {
		return xerrors.Errorf("invalid run configuration: %w", err)
	}
	if err := initRuntimeVars(cfg); err != nil {
		return xerrors.Errorf("invalid runtime variables: %w", err)
	}
	return nil
}
