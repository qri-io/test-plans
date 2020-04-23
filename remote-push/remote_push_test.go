package main

import (
	"context"
	"testing"

	"github.com/testground/sdk-go/runtime"
)

func TestRemotePush(t *testing.T) {
	plan := &Plan{
		Runenv: &runtime.RunEnv{},
	}
	if err := RunPlanRemotePushPull(context.Background(), plan); err != nil {
		t.Error(err)
	}
}
