package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/qri-io/test-plans/plan"
	"github.com/testground/sdk-go/runtime"
)

func main() {
	runtime.Invoke(run)
}

func run(runenv *runtime.RunEnv) error {
	ctx := context.Background()
	p := plan.NewPlan(ctx, runenv)
	ctx, done := context.WithTimeout(ctx, p.Cfg.Timeout)
	defer func() {
		p.Client.Close()
		done()
	}()

	switch c := runenv.TestCase; c {
	case "push":
		return RunPlanRemotePushPull(ctx, p)
	case "pull":
		return RunPlanRemotePull(ctx, p)
	// case "profileExchange":
	// return RunPlanProfileExchange(ctx, p)
	default:
		msg := fmt.Sprintf("Unknown TestCase %s", c)
		return errors.New(msg)
	}
}
