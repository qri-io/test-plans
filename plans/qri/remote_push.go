package main

import (
	"context"

	"github.com/ipfs/testground/plans/qri/sim"

	"github.com/qri-io/qri/event"
	"github.com/qri-io/qri/repo/profile"
)

// RunPlanRemotePushPull demonstrates test output functions
// This method emits two Messages and one Metric
func RunPlanRemotePushPull(ctx context.Context, plan *Plan) error {
	if err := plan.SetupNetwork(ctx); err != nil {
		return err
	}

	var constructor ActorConstructor
	// even actors push, odd actors receive
	if plan.Runenv.TestInstanceCount%2 == 0 {
		constructor = newPusher
	} else {
		constructor = newReceiver
	}

	if err := plan.ConstructActor(ctx, constructor); err != nil {
		return err
	}

	if err := plan.ConnectAllNodes(ctx); err != nil {
		return err
	}

	return <-plan.Finished(ctx)
}

func newPusher(ctx context.Context, plan *Plan) (*sim.Actor, error) {
	opt := func(cfg *sim.Config) {
		// pusher doesn't accept datasets
		cfg.QriConfig.Remote.Enabled = false

		cfg.EventHandlers = map[event.Topic]func(interface{}){
			event.ETP2PQriPeerConnectedEvent: func(payload interface{}) {
				if pro, ok := payload.(*profile.Profile); ok {
					plan.Runenv.RecordMessage("peer connected! %#v", pro)
					// TODO (b5) - attempt to publish to peer just connected to
					plan.ActorFinished(ctx)
				}
				// wait <- fmt.Errorf("%q didn't emit a profile.Profile payload", event.ETP2PQriPeerConnectedEvent)
			},
		}
	}

	act, err := sim.NewActor(ctx, plan.Runenv, opt)
	if err != nil {
		return nil, err
	}

	if err := act.GenerateDatasetVersion("megajoules", 1000); err != nil {
		return nil, err
	}

	if err := act.Inst.Connect(ctx); err != nil {
		return nil, err
	}

	plan.Runenv.RecordMessage("I'm a Pusher named %s", act.Peername())
	return act, err
}

func newReceiver(ctx context.Context, plan *Plan) (*sim.Actor, error) {
	opt := func(cfg *sim.Config) {
		cfg.EventHandlers = map[event.Topic]func(interface{}){
			event.ETP2PQriPeerConnectedEvent: func(payload interface{}) {
				if pro, ok := payload.(*profile.Profile); ok {
					plan.Runenv.RecordMessage("peer connected! %#v", pro)
					// TODO (b5) - don't mark finished unti we've received a dataset
					plan.ActorFinished(ctx)
				}
				// wait <- fmt.Errorf("%q didn't emit a profile.Profile payload", event.ETP2PQriPeerConnectedEvent)
			},
		}
	}

	act, err := sim.NewActor(ctx, plan.Runenv, opt)
	if err != nil {
		return nil, err
	}

	if err := act.Inst.Connect(ctx); err != nil {
		return nil, err
	}

	plan.Runenv.RecordMessage("I'm a Receiver named %s", act.Peername())
	return act, err
}
