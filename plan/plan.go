package plan

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/qri-io/test-plans/sim"
	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
	"golang.org/x/sync/errgroup"
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
	Client    sync.Client
	finishedC <-chan error
	Seq       int64

	Actor  *sim.Actor
	Others map[string]*sim.ActorInfo
}

// NewPlan creates a plan instance from runtime data
func NewPlan(ctx context.Context, runenv *runtime.RunEnv) *Plan {
	client := sync.MustBoundClient(ctx, runenv)
	seq := client.MustSignalAndWait(ctx, "assign-seq", runenv.TestInstanceCount)

	return &Plan{
		Cfg:       PlanConfigFromRuntimeEnv(runenv),
		Runenv:    runenv,
		Client:    client,
		finishedC: client.MustBarrier(ctx, FinishedState, runenv.TestInstanceCount).C,
		Seq:       seq,

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

// ReadyStateConstructed is the state to sync on to ensure all
// actors have been constructed before moving on
var ReadyStateConstructed = sync.State("actor constructed")

// ConstructActor creates an actor and assigns it to the plan
// it's mainly sugar for presnting a uniform plan execution API
func (plan *Plan) ConstructActor(ctx context.Context, constructor ActorConstructor) error {
	var err error
	plan.Actor, err = constructor(ctx, plan)
	// wait until all instances have been constructed
	plan.Client.MustSignalEntry(ctx, ReadyStateConstructed)
	<-plan.Client.MustBarrier(ctx, ReadyStateConstructed, plan.Runenv.TestInstanceCount).C
	return err
}

// ActorInfoTopic represents a subtree under the test run's sync tree
// where peers participating in this distributed test advertise their attributes
var ActorInfoTopic = sync.NewTopic("actor-info", &sim.ActorInfo{})
var ReadyStateActorInfoSync = sync.State("actor info published")

// ShareInfo sends the nodes AddrInfo to all other nodes
// as well as stores the other nodes info in its peerstore
func (plan *Plan) ShareInfo(ctx context.Context) error {
	plan.Runenv.RecordMessage("Getting Actor info: %#v", plan.Actor)
	// get this node's actor info
	actorInfo := plan.Actor.Info(plan.Runenv)

	// send actor infor over the `ActorInfoTopic`
	if _, err := plan.Client.Publish(ctx, ActorInfoTopic, actorInfo); err != nil {
		return fmt.Errorf("publishing ActorInfo: %w", err)
	}

	// wait until all instances have published their actor info
	plan.Client.MustSignalEntry(ctx, ReadyStateActorInfoSync)
	<-plan.Client.MustBarrier(ctx, ReadyStateActorInfoSync, plan.Runenv.TestInstanceCount).C

	// subscribe to the ActorInfoTopic
	actorInfoCh := make(chan *sim.ActorInfo)
	sub, err := plan.Client.Subscribe(ctx, ActorInfoTopic, actorInfoCh)
	if err != nil {
		return fmt.Errorf("node info subscription failure: %w", err)
	}

	// for each node, wait on the actorInfoCh
	// if it is our own actor info, continue
	// otherwise add this info to our plan
	// and add the AddrInfo to our peerstore
	for i := 0; i < plan.Runenv.TestInstanceCount; i++ {
		select {
		case info := <-actorInfoCh:
			if info.ProfileID == actorInfo.ProfileID {
				continue
			}
			// keep record of AddrInfo
			plan.Others[info.ProfileID] = info
			// add AddrInfo to host's peerstore book
			plan.Actor.Inst.Node().Host().Peerstore().AddAddrs(info.AddrInfo.ID, info.AddrInfo.Addrs, peerstore.PermanentAddrTTL)
		case err := <-sub.Done():
			return err
		}
	}

	plan.Runenv.RecordMessage("Shared Info")
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

// DialOtherPeers dials a portion of peers
func (plan *Plan) DialOtherPeers(ctx context.Context) ([]peer.AddrInfo, error) {
	// Grab list of other peers that are available for this Run
	var toDial []peer.AddrInfo
	host := plan.Actor.Inst.Node().Host()
	myID, _ := host.ID().MarshalBinary()

	for _, ai := range plan.Others {
		byteID, _ := ai.AddrInfo.ID.MarshalBinary()

		// skip over dialing ourselves, and prevent TCP simultaneous
		// connect (known to fail) by only dialing peers whose peer ID
		// is smaller than ours.
		if bytes.Compare(byteID, myID) < 0 {
			toDial = append(toDial, *ai.AddrInfo)
		}
	}

	plan.Runenv.RecordMessage("peers I am going to dial: %v", toDial)
	// Dial to all the other peers
	g, ctx := errgroup.WithContext(ctx)
	for _, ai := range toDial {
		ai := ai
		g.Go(func() error {
			if err := host.Connect(ctx, ai); err != nil {
				return fmt.Errorf("Error while dialing peer %v: %w", ai.Addrs, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	plan.Runenv.RecordMessage("dialed other peers")

	return toDial, nil
}
