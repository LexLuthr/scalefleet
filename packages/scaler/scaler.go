package scaler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/actions/scaleset"
	"github.com/actions/scaleset/listener"
	"github.com/google/uuid"
	"golang.org/x/xerrors"
)

type scaleSetAPI interface {
	SystemInfo() scaleset.SystemInfo
	GenerateJitRunnerConfig(ctx context.Context, jitRunnerSetting *scaleset.RunnerScaleSetJitRunnerSetting, runnerScaleSetID int) (*scaleset.RunnerScaleSetJitRunnerConfig, error)
	GetRunner(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error)
	GetRunnerByName(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error)
}

type intentKind string

type intentState string

type runnerIntent struct {
	id            string
	key           string
	kind          intentKind
	state         intentState
	createdAt     time.Time
	updatedAt     time.Time
	submittedAt   time.Time
	nextAttemptAt time.Time
	runnerName    string
	runnerID      int
	zone          string
	lastError     string
}

type controllerOps struct {
	cfg    *Config
	client scaleSetAPI

	reconcileMu sync.Mutex
	intentMu    sync.Mutex

	createIntents    []*runnerIntent
	deleteIntentByID map[string]*runnerIntent
	deleteIntentKeys []string
}

func setupControllerOps(cfg *Config, client *scaleset.Client) *controllerOps {
	return &controllerOps{
		cfg:              cfg,
		client:           client,
		deleteIntentByID: make(map[string]*runnerIntent),
	}
}

func (g *controllerOps) StartOrphanSweeper(ctx context.Context) {
	go func() {
		if err := g.sweepOrphanRunnerVMs(ctx); err != nil && ctx.Err() == nil {
			log.Errorw("Initial orphan sweep failed", "error", err.Error())
		}

		ticker := time.NewTicker(g.cfg.Runtime.OrphanRunnerSweepInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := g.sweepOrphanRunnerVMs(ctx); err != nil && ctx.Err() == nil {
					log.Errorw("Periodic orphan sweep failed", "error", err.Error())
				}
			}
		}
	}()
}

func (g *controllerOps) StartIntentEngine(ctx context.Context) {
	for i := 0; i < g.cfg.Runtime.Worker.WorkerCount; i++ {
		workerID := i + 1
		go g.intentWorker(ctx, workerID)
	}
	go g.confirmSubmittedIntentsLoop(ctx)
}

func (g *controllerOps) intentWorker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		intent := g.claimNextRunnableIntent()
		if intent == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
				continue
			}
		}

		switch intent.kind {
		case intentKindCreate:
			g.processCreateIntent(ctx, workerID, intent)
		case intentKindDelete:
			g.processDeleteIntent(ctx, workerID, intent)
		default:
			g.requeueIntent(intent, g.cfg.Runtime.Worker.RetryBackoff, xerrors.Errorf("unknown intent kind %q", intent.kind))
		}
	}
}

func (g *controllerOps) processCreateIntent(ctx context.Context, workerID int, intent *runnerIntent) {
	scaleSetID := g.client.SystemInfo().ScaleSetID
	seedName := newRunnerName(g.cfg.Runtime.runnerVMPrefix)

	jit, err := callWithRetry(ctx, g.cfg, "GenerateJitRunnerConfig", g.cfg.Runtime.APIReadAttempts, func(callCtx context.Context) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
		return g.client.GenerateJitRunnerConfig(callCtx, &scaleset.RunnerScaleSetJitRunnerSetting{
			Name:       seedName,
			WorkFolder: g.cfg.GitHub.WorkFolder,
		}, scaleSetID)
	})
	if err != nil {
		g.requeueIntent(intent, g.cfg.Runtime.Worker.RetryBackoff, xerrors.Errorf("generate JIT config: %w", err))
		return
	}
	if jit == nil || jit.Runner == nil || strings.TrimSpace(jit.Runner.Name) == "" {
		g.requeueIntent(intent, g.cfg.Runtime.Worker.RetryBackoff, xerrors.New("JIT config missing runner reference"))
		return
	}

	zone, err := g.cfg.createRunnerVM(ctx, jit.EncodedJITConfig, jit.Runner.Name, jit.Runner.ID)
	if err != nil {
		if isZoneCapacityError(err) {
			log.Warnw("Create intent hit zone capacity, scheduling retry",
				"workerID", workerID,
				"runnerName", jit.Runner.Name,
				"runnerID", jit.Runner.ID,
				"cooldown", g.cfg.Runtime.Worker.CapacityRetryCooldown.String(),
				"error", err.Error())
			g.requeueIntent(intent, g.cfg.Runtime.Worker.CapacityRetryCooldown, err)
			return
		}
		g.requeueIntent(intent, g.cfg.Runtime.Worker.RetryBackoff, xerrors.Errorf("create runner VM submit failed: %w", err))
		return
	}

	g.markIntentSubmitted(intent, jit.Runner.Name, jit.Runner.ID, zone)
}

func (g *controllerOps) processDeleteIntent(ctx context.Context, workerID int, intent *runnerIntent) {
	err := g.cfg.deleteRunnerVM(ctx, g.client, intent.runnerName, intent.runnerID)
	if err != nil {
		if strings.Contains(err.Error(), "does not match managed VM prefix") ||
			strings.Contains(err.Error(), "identity mismatch") ||
			strings.Contains(err.Error(), "runner zone metadata mismatch") {
			log.Errorw("Delete intent rejected by identity validation; dropping intent",
				"workerID", workerID,
				"runnerName", intent.runnerName,
				"runnerID", intent.runnerID,
				"error", err.Error())
			g.removeIntent(intent)
			return
		}
		g.requeueIntent(intent, g.cfg.Runtime.Worker.RetryBackoff, xerrors.Errorf("delete runner VM submit failed: %w", err))
		return
	}

	g.markIntentSubmitted(intent, intent.runnerName, intent.runnerID, intent.zone)
}

func (g *controllerOps) confirmSubmittedIntentsLoop(ctx context.Context) {
	ticker := time.NewTicker(g.cfg.Runtime.Worker.ConfirmInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.confirmSubmittedIntents(ctx)
		}
	}
}

func (g *controllerOps) confirmSubmittedIntents(ctx context.Context) {
	now := time.Now()
	intents := g.snapshotSubmittedIntents()
	for _, intent := range intents {
		switch intent.kind {
		case intentKindCreate:
			_, err := g.cfg.getRunnerVMByName(ctx, intent.runnerName)
			if err == nil {
				g.removeIntent(intent)
				continue
			}
			if errors.Is(err, errRunnerVMNotFound) {
				if now.Sub(intent.submittedAt) >= g.cfg.Runtime.Worker.UnknownStateTimeout {
					log.Warnw("Create intent confirmation timed out; dropping intent to allow replan",
						"runnerName", intent.runnerName,
						"runnerID", intent.runnerID,
						"timeout", g.cfg.Runtime.Worker.UnknownStateTimeout.String())
					g.removeIntent(intent)
				}
				continue
			}
			if err != nil {
				log.Warnw("Create intent confirmation lookup failed",
					"runnerName", intent.runnerName,
					"runnerID", intent.runnerID,
					"error", err.Error())
			}
		case intentKindDelete:
			_, err := g.cfg.getRunnerVMByName(ctx, intent.runnerName)
			if errors.Is(err, errRunnerVMNotFound) {
				g.removeIntent(intent)
				continue
			}
			if err == nil {
				if now.Sub(intent.submittedAt) >= g.cfg.Runtime.Worker.UnknownStateTimeout {
					log.Warnw("Delete intent confirmation timed out; requeueing delete",
						"runnerName", intent.runnerName,
						"runnerID", intent.runnerID,
						"timeout", g.cfg.Runtime.Worker.UnknownStateTimeout.String())
					g.requeueIntent(intent, g.cfg.Runtime.Worker.RetryBackoff, xerrors.New("delete not confirmed before timeout"))
				}
				continue
			}
			log.Warnw("Delete intent confirmation lookup failed",
				"runnerName", intent.runnerName,
				"runnerID", intent.runnerID,
				"error", err.Error())
		}
	}
}

func (g *controllerOps) snapshotSubmittedIntents() []*runnerIntent {
	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	out := make([]*runnerIntent, 0, len(g.createIntents)+len(g.deleteIntentByID))
	for _, intent := range g.createIntents {
		if intent != nil && intent.state == intentStateSubmitted {
			out = append(out, intent)
		}
	}
	for _, key := range g.deleteIntentKeys {
		intent := g.deleteIntentByID[key]
		if intent != nil && intent.state == intentStateSubmitted {
			out = append(out, intent)
		}
	}
	return out
}

func (g *controllerOps) claimNextRunnableIntent() *runnerIntent {
	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	now := time.Now()

	for _, key := range g.deleteIntentKeys {
		intent := g.deleteIntentByID[key]
		if intent == nil {
			continue
		}
		if intent.state == intentStateQueued && !now.Before(intent.nextAttemptAt) {
			intent.state = intentStateInFlight
			intent.updatedAt = now
			return intent
		}
	}

	for _, intent := range g.createIntents {
		if intent == nil {
			continue
		}
		if intent.state == intentStateQueued && !now.Before(intent.nextAttemptAt) {
			intent.state = intentStateInFlight
			intent.updatedAt = now
			return intent
		}
	}

	return nil
}

func (g *controllerOps) requeueIntent(intent *runnerIntent, delay time.Duration, err error) {
	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	now := time.Now()
	intent.state = intentStateQueued
	intent.nextAttemptAt = now.Add(delay)
	intent.updatedAt = now
	if err != nil {
		intent.lastError = err.Error()
	}
}

func (g *controllerOps) markIntentSubmitted(intent *runnerIntent, runnerName string, runnerID int, zone string) {
	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	now := time.Now()
	intent.state = intentStateSubmitted
	intent.runnerName = runnerName
	intent.runnerID = runnerID
	intent.zone = zone
	intent.submittedAt = now
	intent.updatedAt = now
	intent.lastError = ""
}

func (g *controllerOps) removeIntent(intent *runnerIntent) {
	g.intentMu.Lock()
	defer g.intentMu.Unlock()
	g.removeIntentLocked(intent)
}

func (g *controllerOps) removeIntentLocked(intent *runnerIntent) {
	switch intent.kind {
	case intentKindCreate:
		for i, candidate := range g.createIntents {
			if candidate == intent {
				g.createIntents = append(g.createIntents[:i], g.createIntents[i+1:]...)
				return
			}
		}
	case intentKindDelete:
		delete(g.deleteIntentByID, intent.key)
		for i, key := range g.deleteIntentKeys {
			if key == intent.key {
				g.deleteIntentKeys = append(g.deleteIntentKeys[:i], g.deleteIntentKeys[i+1:]...)
				return
			}
		}
	}
}

func (g *controllerOps) enqueueCreateIntents(count int) {
	if count <= 0 {
		return
	}

	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	now := time.Now()
	for i := 0; i < count; i++ {
		g.createIntents = append(g.createIntents, &runnerIntent{
			id:            uuid.NewString(),
			kind:          intentKindCreate,
			state:         intentStateQueued,
			createdAt:     now,
			updatedAt:     now,
			nextAttemptAt: now,
		})
	}
}

func (g *controllerOps) enqueueDeleteIntent(runnerName string, runnerID int, reason string) bool {
	runnerName = strings.TrimSpace(runnerName)
	if runnerName == "" {
		return false
	}
	key := fmt.Sprintf("%s:%d", runnerName, runnerID)

	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	if g.deleteIntentByID == nil {
		g.deleteIntentByID = make(map[string]*runnerIntent)
	}
	if _, exists := g.deleteIntentByID[key]; exists {
		return false
	}

	now := time.Now()
	intent := &runnerIntent{
		id:            uuid.NewString(),
		key:           key,
		kind:          intentKindDelete,
		state:         intentStateQueued,
		createdAt:     now,
		updatedAt:     now,
		nextAttemptAt: now,
		runnerName:    runnerName,
		runnerID:      runnerID,
		lastError:     reason,
	}
	g.deleteIntentByID[key] = intent
	g.deleteIntentKeys = append(g.deleteIntentKeys, key)
	return true
}

func (g *controllerOps) cancelQueuedCreateIntents(count int) int {
	if count <= 0 {
		return 0
	}

	g.intentMu.Lock()
	defer g.intentMu.Unlock()

	cancelled := 0
	for i := len(g.createIntents) - 1; i >= 0 && cancelled < count; i-- {
		intent := g.createIntents[i]
		if intent == nil || intent.state != intentStateQueued {
			continue
		}
		g.createIntents = append(g.createIntents[:i], g.createIntents[i+1:]...)
		cancelled++
	}
	return cancelled
}

func (g *controllerOps) intentCounts() (createCount, deleteCount int) {
	g.intentMu.Lock()
	defer g.intentMu.Unlock()
	return len(g.createIntents), len(g.deleteIntentByID)
}

var _ listener.Scaler = (*controllerOps)(nil)

func (g *controllerOps) HandleJobStarted(_ context.Context, jobInfo *scaleset.JobStarted) error {
	if jobInfo == nil {
		return xerrors.New("job started payload is nil")
	}
	log.Infow("Job started", "runnerID", jobInfo.RunnerID, "runnerName", jobInfo.RunnerName)
	return nil
}

func (g *controllerOps) HandleJobCompleted(_ context.Context, jobInfo *scaleset.JobCompleted) error {
	if jobInfo == nil {
		return xerrors.New("job completed payload is nil")
	}
	if strings.TrimSpace(jobInfo.RunnerName) == "" {
		return xerrors.New("job completed payload missing runner name")
	}

	queued := g.enqueueDeleteIntent(jobInfo.RunnerName, jobInfo.RunnerID, "job_completed")
	if queued {
		log.Infow("Job completed delete intent queued", "runnerID", jobInfo.RunnerID, "runnerName", jobInfo.RunnerName, "result", jobInfo.Result)
		return nil
	}
	log.Infow("Job completed delete intent already queued", "runnerID", jobInfo.RunnerID, "runnerName", jobInfo.RunnerName, "result", jobInfo.Result)
	return nil
}

func (g *controllerOps) HandleDesiredRunnerCount(ctx context.Context, desiredCount int) (int, error) {
	if desiredCount < 0 {
		return 0, xerrors.New("desired runner count cannot be negative")
	}

	g.reconcileMu.Lock()
	defer g.reconcileMu.Unlock()

	current, err := g.cfg.countRunnerVMs(ctx)
	if err != nil {
		return current, xerrors.Errorf("count runner VMs: %w", err)
	}

	createIntents, deleteIntents := g.intentCounts()
	effectiveCurrent := current + createIntents - deleteIntents
	if effectiveCurrent >= desiredCount {
		if effectiveCurrent > desiredCount {
			excessCreates := effectiveCurrent - desiredCount
			cancelled := g.cancelQueuedCreateIntents(excessCreates)
			effectiveCurrent -= cancelled
			log.Infow("Current VM count exceeds desired count, waiting for completions to drain",
				"current", current,
				"createIntents", createIntents,
				"deleteIntents", deleteIntents,
				"cancelledQueuedCreates", cancelled,
				"desired", desiredCount)
		}
		return effectiveCurrent, nil
	}

	toCreate := desiredCount - effectiveCurrent
	g.enqueueCreateIntents(toCreate)
	return effectiveCurrent + toCreate, nil
}

func newRunnerName(vmPrefix string) string {
	rawID := strings.ReplaceAll(uuid.NewString(), "-", "")
	return vmPrefix + "-" + rawID[:12]
}

func (g *controllerOps) sweepOrphanRunnerVMs(ctx context.Context) error {
	vms, err := g.cfg.listManagedRunnerVMs(ctx)
	if err != nil {
		return xerrors.Errorf("list managed runner VMs: %w", err)
	}

	now := time.Now()
	for _, vm := range vms {
		if !vm.HasIdentity {
			log.Warnw("Skipping managed VM with invalid runner identity metadata",
				"instanceName", vm.Name)
			continue
		}
		if vm.RunnerName != vm.Name {
			log.Warnw("Skipping managed VM with runner-name metadata mismatch",
				"instanceName", vm.Name,
				"runnerName", vm.RunnerName)
			continue
		}

		if !vm.HasCreationTime {
			log.Warnw("Skipping managed VM with invalid creation timestamp",
				"instanceName", vm.Name)
			continue
		}
		age := runnerVMAge(vm, now)
		if age < g.cfg.Runtime.OrphanRunnerGracePeriod {
			continue
		}

		missingFromScaleSet, err := isRunnerMissingFromScaleSet(ctx, g.cfg, g.client, vm.RunnerName, vm.RunnerID)
		if err != nil {
			log.Warnw("Skipping orphan delete due to scaleset lookup error",
				"instanceName", vm.Name,
				"runnerName", vm.RunnerName,
				"runnerID", vm.RunnerID,
				"error", err.Error())
			continue
		}
		if !missingFromScaleSet {
			continue
		}

		if queued := g.enqueueDeleteIntent(vm.RunnerName, vm.RunnerID, "orphan_sweep"); !queued {
			log.Infow("Orphan delete intent already queued",
				"instanceName", vm.Name,
				"runnerName", vm.RunnerName,
				"runnerID", vm.RunnerID)
			continue
		}

		log.Infow("Queued orphan runner VM delete intent",
			"instanceName", vm.Name,
			"runnerName", vm.RunnerName,
			"runnerID", vm.RunnerID,
			"age", age.String(),
			"reason", "runner missing from scaleset")
	}

	return nil
}
