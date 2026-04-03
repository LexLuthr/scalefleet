package scaler

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/actions/scaleset"
)

func TestIsRunnerMissingFromScaleSetRejectsNilClient(t *testing.T) {
	cfg := newTestConfig()
	missing, err := isRunnerMissingFromScaleSet(context.Background(), cfg, nil, "scalefleet-ci-runner-1", 101)
	if err == nil {
		t.Fatal("expected error for nil scaleset client")
	}
	if missing {
		t.Fatal("missing should be false when input is invalid")
	}
	if !strings.Contains(err.Error(), "scaleset client is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsRunnerMissingFromScaleSetWhenLookupReturnsNilRunner(t *testing.T) {
	cfg := newTestConfig()
	client := &fakeScaleSetClient{
		getRunnerByNameFn: func(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error) {
			return nil, nil
		},
	}

	missing, err := isRunnerMissingFromScaleSet(context.Background(), cfg, client, "scalefleet-ci-runner-1", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !missing {
		t.Fatal("expected runner to be treated as missing when lookup returns nil")
	}
}

func TestIsRunnerMissingFromScaleSetWhenLookupReturnsDifferentRunnerID(t *testing.T) {
	cfg := newTestConfig()
	client := &fakeScaleSetClient{
		getRunnerByNameFn: func(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error) {
			return &scaleset.RunnerReference{Name: runnerName, ID: 999}, nil
		},
	}

	missing, err := isRunnerMissingFromScaleSet(context.Background(), cfg, client, "scalefleet-ci-runner-1", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !missing {
		t.Fatal("expected runner to be treated as missing when runner ID differs")
	}
}

func TestIsRunnerMissingFromScaleSetWhenLookupMatchesRunner(t *testing.T) {
	cfg := newTestConfig()
	client := &fakeScaleSetClient{
		getRunnerByNameFn: func(ctx context.Context, runnerName string) (*scaleset.RunnerReference, error) {
			return &scaleset.RunnerReference{Name: runnerName, ID: 101}, nil
		},
	}

	missing, err := isRunnerMissingFromScaleSet(context.Background(), cfg, client, "scalefleet-ci-runner-1", 101)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing {
		t.Fatal("expected runner to be treated as present when name/ID match")
	}
}

func TestIsNotFoundScaleSetErr(t *testing.T) {
	if !isNotFoundScaleSetErr(errors.New("runner not found")) {
		t.Fatal("expected not-found detection by message")
	}
	if !isNotFoundScaleSetErr(errors.New("HTTP 404")) {
		t.Fatal("expected not-found detection by 404 code")
	}
	if isNotFoundScaleSetErr(errors.New("permission denied")) {
		t.Fatal("unexpected not-found detection for unrelated error")
	}
}
