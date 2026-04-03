package main

import (
	"strings"

	"github.com/LexLuthr/scalefleet/packages/providers/gcp"
	"github.com/LexLuthr/scalefleet/packages/scaler"
	"github.com/urfave/cli/v2"
)

const (
	// GCP flags.
	flagProjectID         = "project-id"
	flagRunnerImage       = "runner-image"
	flagRunnerMachineType = "runner-machine-type"
	flagRunnerNetwork     = "runner-network"
	flagRunnerSAEmail     = "runner-sa-email"
	flagRunnerNetworkTag  = "runner-network-tag"

	// GitHub flags.
	flagGitHubConfigURL               = "github-config-url"
	flagGitHubAuthPATMode             = "github-auth-pat-mode"
	flagPATSecretName                 = "pat-secret-name"
	flagGitHubAppClientIDSecretName   = "github-app-client-id-secret-name"
	flagGitHubAppInstallIDSecretName  = "github-app-installation-id-secret-name"
	flagGitHubAppPrivateKeySecretName = "github-app-private-key-secret-name"
)

func runGCPConfigFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     flagProjectID,
			Usage:    "GCP project ID used for runner VM lifecycle and Secret Manager",
			Required: true,
		},
		&cli.StringFlag{
			Name:     flagRunnerImage,
			Usage:    "Source image for runner VMs (custom image path or family path)",
			Required: true,
		},
		&cli.StringFlag{
			Name:  flagRunnerMachineType,
			Usage: "Runner machine type used for this controller instance (for example: e2-standard-8)",
			Value: "e2-standard-8",
		},
		&cli.StringFlag{
			Name:     flagRunnerNetwork,
			Usage:    "Runner VM network path (for example: global/networks/default)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     flagRunnerSAEmail,
			Usage:    "Service account email attached to runner VMs",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:  flagRunnerNetworkTag,
			Usage: "Optional network tag attached to runner VMs (for example: iap-ssh)",
		},
		&cli.StringFlag{
			Name:  flagGitHubConfigURL,
			Usage: "GitHub repo or org config URL used for scale set registration",
			Value: "https://github.com/LexLuthr/scalefleet",
		},
		&cli.BoolFlag{
			Name:  flagGitHubAuthPATMode,
			Usage: "Enable GitHub PAT authentication mode",
			Value: true,
		},
		&cli.StringFlag{
			Name:  flagPATSecretName,
			Usage: "Secret Manager secret name for GitHub PAT",
		},
		&cli.StringFlag{
			Name:  flagGitHubAppClientIDSecretName,
			Usage: "Secret Manager secret name for GitHub App client ID (when GitHub App mode is used)",
		},
		&cli.StringFlag{
			Name:  flagGitHubAppInstallIDSecretName,
			Usage: "Secret Manager secret name for GitHub App installation ID (when GitHub App mode is used)",
		},
		&cli.StringFlag{
			Name:  flagGitHubAppPrivateKeySecretName,
			Usage: "Secret Manager secret name for GitHub App private key (when GitHub App mode is used)",
		},
	}
}

func applyRunGCPConfigFromFlags(cctx *cli.Context) *scaler.Config {
	// Get GCP flag values
	projectID := strings.TrimSpace(cctx.String(flagProjectID))
	runnerImage := strings.TrimSpace(cctx.String(flagRunnerImage))
	runnerMachineType := strings.TrimSpace(cctx.String(flagRunnerMachineType))
	runnerNetwork := strings.TrimSpace(cctx.String(flagRunnerNetwork))
	runnerSAEmail := strings.TrimSpace(cctx.String(flagRunnerSAEmail))
	runnerNetworkTag := cctx.StringSlice(flagRunnerNetworkTag)

	// Get GH flag values
	ghConfigURL := strings.TrimSpace(cctx.String(flagGitHubConfigURL))
	gitHubAuthPatMode := cctx.Bool(flagGitHubAuthPATMode)
	patSecretName := strings.TrimSpace(cctx.String(flagPATSecretName))
	githubAppClientIDSecretName := strings.TrimSpace(cctx.String(flagGitHubAppClientIDSecretName))
	githubAppInstallationIDSecretName := strings.TrimSpace(cctx.String(flagGitHubAppInstallIDSecretName))
	githubAppPrivateKeySecretName := strings.TrimSpace(cctx.String(flagGitHubAppPrivateKeySecretName))

	cfg := scaler.DefaultConfig()
	providerCfg := gcp.DefaultConfig()
	providerCfg.ProjectID = projectID
	providerCfg.Runner.Image = runnerImage
	providerCfg.Runner.MachineType = runnerMachineType
	providerCfg.Runner.Network = runnerNetwork
	providerCfg.Runner.ServiceAccountEmail = runnerSAEmail
	providerCfg.Runner.NetworkTags = runnerNetworkTag
	providerCfg.Zones = []string{
		runnerZoneUSCentral1A,
		runnerZoneUSCentral1B,
		runnerZoneUSCentral1C,
		runnerZoneUSCentral1F,
	}
	cfg.Provider = providerCfg
	cfg.GitHub.ConfigURL = ghConfigURL
	cfg.GitHub.Auth.PATMode = gitHubAuthPatMode
	cfg.GitHub.Auth.PATSecretName = patSecretName
	cfg.GitHub.Auth.AppClientIDSecretName = githubAppClientIDSecretName
	cfg.GitHub.Auth.AppInstallationIDSecretName = githubAppInstallationIDSecretName
	cfg.GitHub.Auth.AppPrivateKeySecretName = githubAppPrivateKeySecretName

	return &cfg
}

const (
	runnerZoneUSCentral1A = "us-central1-a"
	runnerZoneUSCentral1B = "us-central1-b"
	runnerZoneUSCentral1C = "us-central1-c"
	runnerZoneUSCentral1F = "us-central1-f"
)

var runGCPCommand = &cli.Command{
	Name:  "run-gcp",
	Usage: "Run the controller using the GCP provider",
	Flags: runGCPConfigFlags(),
	Action: func(cctx *cli.Context) error {
		cfg := applyRunGCPConfigFromFlags(cctx)
		return scaler.Run(cctx.Context, cfg)
	},
}
