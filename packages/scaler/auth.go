package scaler

import (
	"context"
	"strconv"
	"strings"

	"github.com/actions/scaleset"
	"golang.org/x/xerrors"
)

type scaleSetClientFactory interface {
	NewClientWithPersonalAccessToken(cfg scaleset.NewClientWithPersonalAccessTokenConfig) (*scaleset.Client, error)
	NewClientWithGitHubApp(cfg scaleset.ClientWithGitHubAppConfig) (*scaleset.Client, error)
}

type defaultScaleSetClientFactory struct{}

func (defaultScaleSetClientFactory) NewClientWithPersonalAccessToken(cfg scaleset.NewClientWithPersonalAccessTokenConfig) (*scaleset.Client, error) {
	return scaleset.NewClientWithPersonalAccessToken(cfg)
}

func (defaultScaleSetClientFactory) NewClientWithGitHubApp(cfg scaleset.ClientWithGitHubAppConfig) (*scaleset.Client, error) {
	return scaleset.NewClientWithGitHubApp(cfg)
}

func createScaleSetClient(ctx context.Context, factory scaleSetClientFactory, cfg *Config, systemInfo scaleset.SystemInfo) (*scaleset.Client, error) {
	if cfg.GitHub.Auth.PATMode {
		pat, err := cfg.loadPATFromSecretManager(ctx)
		if err != nil {
			return nil, xerrors.Errorf("load PAT from secret manager: %w", err)
		}
		return factory.NewClientWithPersonalAccessToken(scaleset.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     cfg.GitHub.ConfigURL,
			PersonalAccessToken: pat,
			SystemInfo:          systemInfo,
		})
	}

	appAuth, err := cfg.loadGitHubAppAuthFromSecretManager(ctx)
	if err != nil {
		return nil, xerrors.Errorf("load GitHub App auth from secret manager: %w", err)
	}
	return factory.NewClientWithGitHubApp(scaleset.ClientWithGitHubAppConfig{
		GitHubConfigURL: cfg.GitHub.ConfigURL,
		GitHubAppAuth:   appAuth,
		SystemInfo:      systemInfo,
	})

}

func (cfg *Config) loadPATFromSecretManager(ctx context.Context) (string, error) {
	return cfg.loadSecretValue(ctx, cfg.GitHub.Auth.PATSecretName)
}

func (cfg *Config) loadGitHubAppAuthFromSecretManager(ctx context.Context) (scaleset.GitHubAppAuth, error) {

	clientID, err := cfg.loadSecretValue(ctx, cfg.GitHub.Auth.AppClientIDSecretName)
	if err != nil {
		return scaleset.GitHubAppAuth{}, xerrors.Errorf("load app client id: %w", err)
	}
	installationIDRaw, err := cfg.loadSecretValue(ctx, cfg.GitHub.Auth.AppInstallationIDSecretName)
	if err != nil {
		return scaleset.GitHubAppAuth{}, xerrors.Errorf("load app installation id: %w", err)
	}
	installationID, err := strconv.ParseInt(installationIDRaw, 10, 64)
	if err != nil {
		return scaleset.GitHubAppAuth{}, xerrors.Errorf("parse app installation id: %w", err)
	}
	privateKey, err := cfg.loadSecretValue(ctx, cfg.GitHub.Auth.AppPrivateKeySecretName)
	if err != nil {
		return scaleset.GitHubAppAuth{}, xerrors.Errorf("load app private key: %w", err)
	}

	return scaleset.GitHubAppAuth{
		ClientID:       clientID,
		InstallationID: installationID,
		PrivateKey:     privateKey,
	}, nil
}

func (cfg *Config) loadSecretValue(ctx context.Context, secretName string) (string, error) {
	provider, err := cfg.provider()
	if err != nil {
		return "", err
	}
	secret, err := callWithRetry(ctx, cfg, "SecretManagerAccess", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (string, error) {
		return provider.LoadSecretValue(callCtx, secretName)
	})
	if err != nil {
		return "", err
	}

	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", xerrors.New("secret value is empty")
	}
	return secret, nil
}
