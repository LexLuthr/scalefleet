package scaler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/actions/scaleset"
)

type fakeClientFactory struct {
	patClientFn       func(cfg scaleset.NewClientWithPersonalAccessTokenConfig) (*scaleset.Client, error)
	githubAppClientFn func(cfg scaleset.ClientWithGitHubAppConfig) (*scaleset.Client, error)
}

func (f fakeClientFactory) NewClientWithPersonalAccessToken(cfg scaleset.NewClientWithPersonalAccessTokenConfig) (*scaleset.Client, error) {
	if f.patClientFn == nil {
		return nil, errors.New("unexpected NewClientWithPersonalAccessToken call")
	}
	return f.patClientFn(cfg)
}

func (f fakeClientFactory) NewClientWithGitHubApp(cfg scaleset.ClientWithGitHubAppConfig) (*scaleset.Client, error) {
	if f.githubAppClientFn == nil {
		return nil, errors.New("unexpected NewClientWithGitHubApp call")
	}
	return f.githubAppClientFn(cfg)
}

func TestCreateScaleSetClientPATLoadError(t *testing.T) {
	defaults := DefaultConfig()
	cfg := &Config{
		Provider: &testProvider{
			secretFn: func(ctx context.Context, secretName string) (string, error) {
				return "", errors.New("secret load failed")
			},
		},
		GitHub: GitHubConfig{
			Auth: GitHubAuthConfig{
				PATMode:       true,
				PATSecretName: "PAT_something",
			},
		},
		Runtime: RuntimeConfig{
			APICallTimeout:    defaults.Runtime.APICallTimeout,
			APIInitialBackoff: defaults.Runtime.APIInitialBackoff,
			APIReadAttempts:   1,
		},
	}

	_, err := createScaleSetClient(context.Background(), fakeClientFactory{
		patClientFn: func(cfg scaleset.NewClientWithPersonalAccessTokenConfig) (*scaleset.Client, error) {
			t.Fatal("PAT client constructor must not run when PAT secret load fails")
			return nil, nil
		},
	}, cfg, scaleset.SystemInfo{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "load PAT from secret manager") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateScaleSetClientGitHubAppLoadError(t *testing.T) {
	defaults := DefaultConfig()
	cfg := &Config{
		Provider: &testProvider{
			secretFn: func(ctx context.Context, secretName string) (string, error) {
				return "", errors.New("secret load failed")
			},
		},
		GitHub: GitHubConfig{
			Auth: GitHubAuthConfig{
				PATMode:                     false,
				AppClientIDSecretName:       "app-client-id",
				AppInstallationIDSecretName: "app-installation-id",
				AppPrivateKeySecretName:     "app-private-key",
			},
		},
		Runtime: RuntimeConfig{
			APICallTimeout:    defaults.Runtime.APICallTimeout,
			APIInitialBackoff: defaults.Runtime.APIInitialBackoff,
			APIReadAttempts:   1,
		},
	}

	_, err := createScaleSetClient(context.Background(), fakeClientFactory{
		githubAppClientFn: func(cfg scaleset.ClientWithGitHubAppConfig) (*scaleset.Client, error) {
			t.Fatal("GitHub App client constructor must not run when app secret load fails")
			return nil, nil
		},
	}, cfg, scaleset.SystemInfo{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "load GitHub App auth from secret manager") {
		t.Fatalf("unexpected error: %v", err)
	}
}
