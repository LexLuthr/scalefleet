package scaler

import (
	"context"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("scalefleet")

// Run executes the scalefleet controller with a caller-supplied config.
func Run(ctx context.Context, cfg *Config) error {
	if err := cfg.validate(); err != nil {
		return xerrors.Errorf("invalid run configuration: %w", err)
	}
	if err := initRuntimeVars(cfg); err != nil {
		return xerrors.Errorf("invalid runtime variables: %w", err)
	}
	return run(ctx, cfg)
}

func run(ctx context.Context, cfg *Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	systemInfo := scaleset.SystemInfo{
		System:    cfg.Build.System,
		Version:   cfg.Build.Version,
		CommitSHA: cfg.Build.CommitSHA,
	}

	client, err := createScaleSetClient(ctx, defaultScaleSetClientFactory{}, cfg, systemInfo)
	if err != nil {
		return xerrors.Errorf("failed to create scaleset client: %w", err)
	}

	rg, err := callWithRetry(ctx, cfg, "GetRunnerGroupByName", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerGroup, error) {
		return client.GetRunnerGroupByName(callCtx, cfg.GitHub.RunnerGroupName)
	})
	if err != nil {
		return xerrors.Errorf("failed to get runner group: %w", err)
	}

	ss, err := ensureRunnerScaleSet(ctx, cfg, client, rg, cfg.Runtime.scaleSetName)
	if err != nil {
		return err
	}

	client.SetSystemInfo(scaleset.SystemInfo{
		System:     cfg.Build.System,
		Version:    cfg.Build.Version,
		CommitSHA:  cfg.Build.CommitSHA,
		ScaleSetID: ss.ID,
	})

	defer func() {
		log.Infow("Deleting runner scale set", "scaleSetID", ss.ID)
		if err := client.DeleteRunnerScaleSet(context.WithoutCancel(ctx), ss.ID); err != nil {
			log.Errorw("Failed to delete runner scale set", "scaleSetID", ss.ID, "error", err.Error())
		}
	}()

	sessionOwner := uuid.NewString()

	sessionClient, err := callWithRetry(ctx, cfg, "MessageSessionClient", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.MessageSessionClient, error) {
		return client.MessageSessionClient(callCtx, ss.ID, sessionOwner)
	})
	if err != nil {
		return xerrors.Errorf("failed to create message session client: %w", err)
	}

	scaleSetListener, err := listener.New(sessionClient, listener.Config{
		ScaleSetID: ss.ID,
		MaxRunners: cfg.ScaleSet.MaxRunners,
	})
	if err != nil {
		return xerrors.Errorf("failed to create listener: %w", err)
	}

	ops := setupControllerOps(cfg, client)
	ops.StartIntentEngine(ctx)
	ops.StartOrphanSweeper(ctx)

	if err := scaleSetListener.Run(ctx, ops); err != nil {
		return xerrors.Errorf("listener run failed: %w", err)
	}
	return nil
}
