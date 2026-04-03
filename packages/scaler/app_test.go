package scaler

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/actions/scaleset"
)

type fakeScaleSetClient struct {
	systemInfo        scaleset.SystemInfo
	generateJITFn     func(ctx context.Context, setting *scaleset.RunnerScaleSetJitRunnerSetting, runnerScaleSetID int) (*scaleset.RunnerScaleSetJitRunnerConfig, error)
	getRunnerFn       func(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error)
	getRunnerByNameFn func(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error)
}

func (f *fakeScaleSetClient) SystemInfo() scaleset.SystemInfo {
	return f.systemInfo
}

func (f *fakeScaleSetClient) GenerateJitRunnerConfig(ctx context.Context, setting *scaleset.RunnerScaleSetJitRunnerSetting, runnerScaleSetID int) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
	if f.generateJITFn == nil {
		return nil, errors.New("unexpected GenerateJitRunnerConfig call")
	}
	return f.generateJITFn(ctx, setting, runnerScaleSetID)
}

func (f *fakeScaleSetClient) GetRunner(ctx context.Context, runnerID int) (*scaleset.RunnerReference, error) {
	if f.getRunnerFn == nil {
		return nil, errors.New("unexpected GetRunner call")
	}
	return f.getRunnerFn(ctx, runnerID)
}

func (f *fakeScaleSetClient) GetRunnerByName(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error) {
	if f.getRunnerByNameFn == nil {
		return nil, errors.New("unexpected GetRunnerByName call")
	}
	return f.getRunnerByNameFn(ctx, runnerName)
}

func TestHandleDesiredRunnerCountRejectsNegativeDesired(t *testing.T) {
	g := &controllerOps{client: &fakeScaleSetClient{}}
	_, err := g.HandleDesiredRunnerCount(context.Background(), -1)
	if err == nil {
		t.Fatal("expected error for negative desired count")
	}
	if !strings.Contains(err.Error(), "desired runner count cannot be negative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleJobStartedRejectsNilPayload(t *testing.T) {
	g := &controllerOps{}
	err := g.HandleJobStarted(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil job payload")
	}
	if !strings.Contains(err.Error(), "job started payload is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleJobStartedAcceptsPayload(t *testing.T) {
	g := &controllerOps{}
	err := g.HandleJobStarted(context.Background(), &scaleset.JobStarted{
		RunnerName: "scalefleet-ci-runner-abc",
		RunnerID:   77,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleJobCompletedValidatesRunnerName(t *testing.T) {
	g := &controllerOps{}
	err := g.HandleJobCompleted(context.Background(), &scaleset.JobCompleted{
		RunnerName: "   ",
		RunnerID:   77,
	})
	if err == nil {
		t.Fatal("expected error for empty runner name")
	}
	if !strings.Contains(err.Error(), "job completed payload missing runner name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleJobCompletedRejectsNilPayload(t *testing.T) {
	g := &controllerOps{}
	err := g.HandleJobCompleted(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil job payload")
	}
	if !strings.Contains(err.Error(), "job completed payload is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleJobCompletedQueuesDeleteIntent(t *testing.T) {
	g := &controllerOps{
		client:           &fakeScaleSetClient{},
		deleteIntentByID: make(map[string]*runnerIntent),
	}
	err := g.HandleJobCompleted(context.Background(), &scaleset.JobCompleted{
		RunnerName: "runner-without-managed-prefix",
		RunnerID:   77,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.deleteIntentByID) != 1 {
		t.Fatalf("expected one queued delete intent, got=%d", len(g.deleteIntentByID))
	}
}

func TestNewRunnerNameUsesPrefixAndLowercaseUUID12(t *testing.T) {
	name := newRunnerName("scalefleet-ci-runner-8cpu")
	const prefix = "scalefleet-ci-runner-8cpu-"

	if !strings.HasPrefix(name, prefix) {
		t.Fatalf("runner name missing expected prefix: %q", name)
	}

	suffix := strings.TrimPrefix(name, prefix)
	if len(suffix) != 12 {
		t.Fatalf("runner suffix length mismatch: got=%d want=12", len(suffix))
	}

	if !regexp.MustCompile(`^[a-z0-9]{12}$`).MatchString(suffix) {
		t.Fatalf("runner suffix has invalid characters: %q", suffix)
	}

	if len(name) > 63 {
		t.Fatalf("runner name exceeds GCE length limit: %d", len(name))
	}
}

func TestNewRunnerNameIsUniqueAcrossBurst(t *testing.T) {
	const prefix = "scalefleet-ci-runner-8cpu"
	const samples = 128

	seen := make(map[string]struct{}, samples)
	for i := 0; i < samples; i++ {
		name := newRunnerName(prefix)
		if _, ok := seen[name]; ok {
			t.Fatalf("duplicate runner name generated: %q", name)
		}
		seen[name] = struct{}{}
	}
}
