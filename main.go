package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/ipfs/testground/sdk/runtime"
)

func main() {
	runtime.Invoke(run)
}

func run(runenv *runtime.RunEnv) error {
	ctx := context.Background()
	plan := NewPlan(ctx, runenv)

	ctx, done := context.WithTimeout(ctx, plan.Cfg.Timeout)
	defer done()

	switch c := runenv.TestCase; c {
	case "remote-push":
		return RunPlanRemotePushPull(ctx, plan)
	default:
		msg := fmt.Sprintf("Unknown TestCase %s", c)
		return errors.New(msg)
	}
}
