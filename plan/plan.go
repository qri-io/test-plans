package plan

import (
	"context"
	"fmt"
	"time"

	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"

	"github.com/qri-io/test-plans/sim"
)

// PlanConfig encapsulates test plan paremeters
type PlanConfig struct {
	Timeout time.Duration
	Latency time.Duration
}

// PlanConfigFromRuntimeEnv parses configuration from the runtime environment
func PlanConfigFromRuntimeEnv(runenv *runtime.RunEnv) *PlanConfig {
	return &PlanConfig{
		Timeout: time.Duration(runenv.IntParam("timeout_secs")) * time.Second,
		Latency: time.Duration(runenv.IntParam("latency")) * time.Millisecond,
	}
}

// Plan holds state for test plan execution, one plan instance is constructed
// per node in the plan, with methods for syncronizing plan instances
// out-of-band from the test itself
type Plan struct {
	Cfg       *PlanConfig
	Runenv    *runtime.RunEnv
	Client    *sync.Client
	finishedC <-chan error

	Actor  *sim.Actor
	Others map[string]*sim.ActorInfo
}

// NewPlan creates a plan instance from runtime data
func NewPlan(ctx context.Context, runenv *runtime.RunEnv) *Plan {
	client := sync.MustBoundClient(ctx, runenv)

	return &Plan{
		Cfg:       PlanConfigFromRuntimeEnv(runenv),
		Runenv:    runenv,
		Client:    client,
		finishedC: client.MustBarrier(ctx, FinishedState, runenv.TestInstanceCount).C,

		Others: map[string]*sim.ActorInfo{},
	}
}

// SetupNetwork configures the network for plan execution
func (plan *Plan) SetupNetwork(ctx context.Context) error {
	if !plan.Runenv.TestSidecar {
		return nil
	}
	// hostname, err := os.Hostname()
	// if err != nil {
	// 	return err
	// }

	ntwkCfg := network.Config{
		// Control the "default" network. At the moment, this is the only network.
		Network: "default",

		// Enable this network. Setting this to false will disconnect this test
		// instance from this network. You probably don't want to do that.
		Enable: true,
		Default: network.LinkShape{
			Latency:   plan.Cfg.Latency,
			Bandwidth: 10 << 20, // 10Mib
		},
		CallbackState: "network-configured",
	}

	// if _, err = plan.Client.Publish(ctx, network.Topic(hostname), &ntwkCfg); err != nil {
	// 	return err
	// }

	// return <-plan.Client.MustBarrier(ctx, ntwkCfg.CallbackState, plan.Runenv.TestInstanceCount).C
	netclient := network.NewClient(plan.Client, plan.Runenv)
	netclient.MustWaitNetworkInitialized(ctx)
	netclient.MustConfigureNetwork(ctx, &ntwkCfg)
	return nil
}

// ActorConstructor is a function that creates an actor
type ActorConstructor func(context.Context, *Plan) (*sim.Actor, error)

// ConstructActor creates an actor and assigns it to the plan
// it's mainly sugar for presnting a uniform plan execution API
func (plan *Plan) ConstructActor(ctx context.Context, constructor ActorConstructor) error {
	var err error
	plan.Actor, err = constructor(ctx, plan)
	return err
}

// ActorInfoTopic represents a subtree under the test run's sync tree
// where peers participating in this distributed test advertise their attributes
var ActorInfoTopic = sync.NewTopic("actor-info", &sim.ActorInfo{})
var ReadyStateActorInfoSync = sync.State("actor info published")

// ShareInfo wires up all nodes to each other
func (plan *Plan) ShareInfo(ctx context.Context) error {
	plan.Runenv.RecordMessage("Getting Actor info: %#v", plan.Actor)
	actorInfo := plan.Actor.Info(plan.Runenv)
	// write our own info

	if _, err := plan.Client.Publish(ctx, ActorInfoTopic, actorInfo); err != nil {
		return fmt.Errorf("publishing ActorInfo: %w", err)
	}

	// wait until all instances have published their actor info
	plan.Client.MustSignalEntry(ctx, ReadyStateActorInfoSync)
	<-plan.Client.MustBarrier(ctx, ReadyStateActorInfoSync, plan.Runenv.TestInstanceCount).C

	actorInfoCh := make(chan *sim.ActorInfo)
	sub, err := plan.Client.Subscribe(ctx, ActorInfoTopic, actorInfoCh)
	if err != nil {
		return fmt.Errorf("node info subscription failure: %w", err)
	}

	for i := 0; i < plan.Runenv.TestInstanceCount; i++ {
		select {
		case info := <-actorInfoCh:
			if info.PeerID == actorInfo.PeerID {
				continue
			}
			plan.Others[info.PeerID] = info
		case err := <-sub.Done():
			return err
		}
	}

	plan.Runenv.RecordMessage("Shared Info")
	return nil
}

// ConnectAllNodes wires each node to the other node
func (plan *Plan) ConnectAllNodes(ctx context.Context) error {
	plan.Runenv.RecordMessage("Connecting to all nodes")
	if err := plan.ShareInfo(ctx); err != nil {
		return err
	}

	h := plan.Actor.Inst.Node().Host()
	for _, info := range plan.Others {
		for _, addr := range info.AddrInfo.Addrs {
			plan.Runenv.RecordMessage("address: %s", addr.String())
		}
		if err := h.Connect(ctx, *info.AddrInfo); err != nil {
			return err
		}
	}
	plan.Runenv.RecordMessage("Connected to all nodes")
	return nil
}

// FinishedState coordinates plan completion
const FinishedState = sync.State("finished")

// ActorFinished marks this actor as having completed their goal for the plan
func (plan *Plan) ActorFinished(ctx context.Context) error {
	plan.Runenv.RecordMessage("Finished")
	// write our oun attribute info
	if _, err := plan.Client.SignalEntry(ctx, FinishedState); err != nil {
		return fmt.Errorf("ActorFinished failure: %w", err)
	}
	return nil
}

// Finished blocks until the plan is finished
func (plan *Plan) Finished(ctx context.Context) <-chan error {
	return plan.finishedC
}

// Close finalizes the plan & cleans up resources
func (plan *Plan) Close() {
	plan.Client.Close()
}
