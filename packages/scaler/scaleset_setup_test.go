package scaler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/actions/scaleset"
)

type fakeRunnerScaleSetAPI struct {
	createFn func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error)
	getFn    func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error)
}

func (f fakeRunnerScaleSetAPI) CreateRunnerScaleSet(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
	if f.createFn == nil {
		return nil, errors.New("unexpected CreateRunnerScaleSet call")
	}
	return f.createFn(ctx, request)
}

func (f fakeRunnerScaleSetAPI) GetRunnerScaleSet(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
	if f.getFn == nil {
		return nil, errors.New("unexpected GetRunnerScaleSet call")
	}
	return f.getFn(ctx, runnerGroupID, runnerScaleSetName)
}

func TestEnsureRunnerScaleSetCreatesNew(t *testing.T) {
	cfg := newTestConfig()
	rg := &scaleset.RunnerGroup{ID: 7, Name: "group"}
	scaleSetName := "scalefleet-ci-scaleset-8cpu"
	created := &scaleset.RunnerScaleSet{ID: 11, Name: scaleSetName}
	getCalls := 0
	createCalls := 0

	got, err := ensureRunnerScaleSetWithAPI(context.Background(), cfg, fakeRunnerScaleSetAPI{
		getFn: func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
			getCalls++
			if runnerGroupID != rg.ID {
				t.Fatalf("unexpected runner group id: %d", runnerGroupID)
			}
			if runnerScaleSetName != scaleSetName {
				t.Fatalf("unexpected scaleset name: %q", runnerScaleSetName)
			}
			return nil, nil
		},
		createFn: func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
			createCalls++
			if request.RunnerGroupID != rg.ID {
				t.Fatalf("unexpected runner group id: %d", request.RunnerGroupID)
			}
			if request.RunnerGroupName != rg.Name {
				t.Fatalf("unexpected runner group name: %q", request.RunnerGroupName)
			}
			if request.Name != scaleSetName {
				t.Fatalf("unexpected scaleset name: %q", request.Name)
			}
			if len(request.Labels) != 1 {
				t.Fatalf("unexpected labels count: got=%d want=1", len(request.Labels))
			}
			if request.Labels[0].Name != scaleSetName {
				t.Fatalf("unexpected scaleset label name: got=%q want=%q", request.Labels[0].Name, scaleSetName)
			}
			if request.Labels[0].Type != "" {
				t.Fatalf("unexpected scaleset label type: got=%q want empty", request.Labels[0].Type)
			}
			return created, nil
		},
	}, rg, scaleSetName)
	if err != nil {
		t.Fatalf("ensureRunnerScaleSetWithAPI returned error: %v", err)
	}
	if got != created {
		t.Fatalf("expected created scaleset pointer")
	}
	if getCalls != 1 {
		t.Fatalf("unexpected get calls: got=%d want=1", getCalls)
	}
	if createCalls != 1 {
		t.Fatalf("unexpected create calls: got=%d want=1", createCalls)
	}
}

func TestEnsureRunnerScaleSetReturnsExistingBeforeCreate(t *testing.T) {
	cfg := newTestConfig()
	rg := &scaleset.RunnerGroup{ID: 7, Name: "group"}
	scaleSetName := "scalefleet-ci-scaleset-8cpu"
	existing := &scaleset.RunnerScaleSet{ID: 33, Name: scaleSetName}
	createCalls := 0

	got, err := ensureRunnerScaleSetWithAPI(context.Background(), cfg, fakeRunnerScaleSetAPI{
		getFn: func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
			return existing, nil
		},
		createFn: func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
			createCalls++
			return nil, errors.New("create should not run when scaleset already exists")
		},
	}, rg, scaleSetName)
	if err != nil {
		t.Fatalf("ensureRunnerScaleSetWithAPI returned error: %v", err)
	}
	if got != existing {
		t.Fatalf("expected existing scaleset pointer")
	}
	if createCalls != 0 {
		t.Fatalf("unexpected create calls: got=%d want=0", createCalls)
	}
}

func TestEnsureRunnerScaleSetReusesExistingOnCreateFailure(t *testing.T) {
	cfg := newTestConfig()
	rg := &scaleset.RunnerGroup{ID: 7, Name: "group"}
	scaleSetName := "scalefleet-ci-scaleset-16cpu"
	existing := &scaleset.RunnerScaleSet{ID: 22, Name: scaleSetName}
	getCalls := 0

	got, err := ensureRunnerScaleSetWithAPI(context.Background(), cfg, fakeRunnerScaleSetAPI{
		createFn: func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
			return nil, errors.New("create failed")
		},
		getFn: func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
			getCalls++
			if runnerGroupID != rg.ID {
				t.Fatalf("unexpected runner group id: %d", runnerGroupID)
			}
			if runnerScaleSetName != scaleSetName {
				t.Fatalf("unexpected scaleset name: %q", runnerScaleSetName)
			}
			if getCalls == 1 {
				return nil, nil
			}
			return existing, nil
		},
	}, rg, scaleSetName)
	if err != nil {
		t.Fatalf("ensureRunnerScaleSetWithAPI returned error: %v", err)
	}
	if got != existing {
		t.Fatalf("expected existing scaleset pointer")
	}
	if getCalls != 2 {
		t.Fatalf("unexpected get calls: got=%d want=2", getCalls)
	}
}

func TestEnsureRunnerScaleSetFailsWhenCreateAndGetFail(t *testing.T) {
	cfg := newTestConfig()
	rg := &scaleset.RunnerGroup{ID: 7, Name: "group"}
	getCalls := 0

	_, err := ensureRunnerScaleSetWithAPI(context.Background(), cfg, fakeRunnerScaleSetAPI{
		createFn: func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
			return nil, errors.New("create failed")
		},
		getFn: func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
			getCalls++
			if getCalls == 1 {
				return nil, nil
			}
			return nil, errors.New("get failed")
		},
	}, rg, "scalefleet-ci-scaleset-4cpu")
	if err == nil {
		t.Fatal("expected error when both create and get fail")
	}
	if !strings.Contains(err.Error(), "create runner scale set failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRunnerScaleSetFailsWhenInitialGetFails(t *testing.T) {
	cfg := newTestConfig()
	rg := &scaleset.RunnerGroup{ID: 7, Name: "group"}

	_, err := ensureRunnerScaleSetWithAPI(context.Background(), cfg, fakeRunnerScaleSetAPI{
		getFn: func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
			return nil, errors.New("get failed")
		},
		createFn: func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
			t.Fatal("create should not run when initial get fails")
			return nil, nil
		},
	}, rg, "scalefleet-ci-scaleset-4cpu")
	if err == nil {
		t.Fatal("expected error when initial get fails")
	}
	if !strings.Contains(err.Error(), "get runner scale set failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRunnerScaleSetFailsWhenPostCreateLookupIsNil(t *testing.T) {
	cfg := newTestConfig()
	rg := &scaleset.RunnerGroup{ID: 7, Name: "group"}
	getCalls := 0

	_, err := ensureRunnerScaleSetWithAPI(context.Background(), cfg, fakeRunnerScaleSetAPI{
		getFn: func(ctx context.Context, runnerGroupID int, runnerScaleSetName string) (*scaleset.RunnerScaleSet, error) {
			getCalls++
			return nil, nil
		},
		createFn: func(ctx context.Context, request *scaleset.RunnerScaleSet) (*scaleset.RunnerScaleSet, error) {
			return nil, errors.New("create failed")
		},
	}, rg, "scalefleet-ci-scaleset-4cpu")
	if err == nil {
		t.Fatal("expected error when post-create lookup returns nil")
	}
	if !strings.Contains(err.Error(), "post-create lookup returned no scale set") {
		t.Fatalf("unexpected error: %v", err)
	}
	if getCalls != 2 {
		t.Fatalf("unexpected get calls: got=%d want=2", getCalls)
	}
}
