package main

import (
	"context"
	"testing"

	"github.com/qri-io/test-plans/plan"
	"github.com/testground/sdk-go/runtime"
)

func TestRemotePush(t *testing.T) {
	plan := &plan.Plan{
		Runenv: &runtime.RunEnv{},
	}
	if err := RunPlanRemotePushPull(context.Background(), plan); err != nil {
		t.Error(err)
	}
}
