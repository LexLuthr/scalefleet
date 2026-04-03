package scaler

import (
	"context"
	"time"

	"github.com/actions/scaleset"
	"golang.org/x/xerrors"
)

type runnerScaleSetAPI interface {
	CreateRunnerScaleSet(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error)
	GetRunnerScaleSet(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error)
}

func ensureRunnerScaleSet(ctx context.Context, cfg *Config, client *scaleset.Client, rg *scaleset.RunnerGroup, scaleSetName string) (*scaleset.RunnerScaleSet, error) {
	return ensureRunnerScaleSetWithAPI(ctx, cfg, client, rg, scaleSetName)
}

func ensureRunnerScaleSetWithAPI(ctx context.Context, cfg *Config, client runnerScaleSetAPI, rg *scaleset.RunnerGroup, scaleSetName string) (*scaleset.RunnerScaleSet, error) {
	existing, err := callWithRetry(ctx, cfg, "GetRunnerScaleSet", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerScaleSet, error) {
		return client.GetRunnerScaleSet(callCtx, rg.ID, scaleSetName)
	})
	if err != nil {
		return nil, xerrors.Errorf("get runner scale set failed: %w", err)
	}
	if existing != nil {
		log.Infow("Reusing existing runner scale set",
			"scaleSetName", scaleSetName,
			"runnerGroupID", rg.ID,
			"scaleSetID", existing.ID)
		return existing, nil
	}

	ss, err := callWithRetry(ctx, cfg, "CreateRunnerScaleSet", cfg.Runtime.APIWriteAttempts, func(callCtx context.Context) (*scaleset.RunnerScaleSet, error) {
		return client.CreateRunnerScaleSet(callCtx, &scaleset.RunnerScaleSet{
			Name:            scaleSetName,
			RunnerGroupID:   rg.ID,
			RunnerGroupName: rg.Name,
			Labels:          []scaleset.Label{{Name: scaleSetName}},
			CreatedOn:       time.Now(),
		})
	})
	if err == nil {
		if ss != nil {
			return ss, nil
		}
	}
	// If create failed but the scale set now exists, reuse it (race or eventual consistency).
	existing1, getErr := callWithRetry(ctx, cfg, "GetRunnerScaleSetAfterCreateFailure", cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerScaleSet, error) {
		return client.GetRunnerScaleSet(callCtx, rg.ID, scaleSetName)
	})
	if getErr != nil {
		return nil, xerrors.Errorf("create runner scale set failed: %w (post-create lookup failed: %w)", err, getErr)
	}

	if existing1 == nil {
		return nil, xerrors.Errorf("create runner scale set failed: %w (post-create lookup returned no scale set)", err)
	}

	createErr := ""
	if err != nil {
		createErr = err.Error()
	}

	log.Warnw("Create runner scale set failed, reusing existing scale set",
		"scaleSetName", scaleSetName,
		"runnerGroupID", rg.ID,
		"scaleSetID", existing1.ID,
		"createError", createErr)
	return existing1, nil
}
