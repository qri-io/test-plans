package main

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/qri-io/qri/config"
	"github.com/qri-io/qri/dsref"
	"github.com/qri-io/qri/event"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/qri/repo/profile"
	"github.com/qri-io/test-plans/plan"
	"github.com/qri-io/test-plans/sim"
	"github.com/testground/sdk-go/sync"
)

var datasetName = "megajoules"
var defaultDatasetSize = 1000
var defaultPushersPerReceiver = 1

// RunPlanRemotePushPull demonstrates test output functions
// This method emits two Messages and one Metric
func RunPlanRemotePushPull(ctx context.Context, p *plan.Plan) error {
	pushersPerReceiver := getPushersPerReceiver(p)
	if pushersPerReceiver >= p.Runenv.TestInstanceCount {
		return fmt.Errorf("Push variable specify %d pusher per receiver, but there are only %d instances", pushersPerReceiver, p.Runenv.TestInstanceCount)
	}
	if err := p.SetupNetwork(ctx); err != nil {
		return err
	}

	isReceiver := p.Seq%(int64(pushersPerReceiver+1)) == 0

	var constructor plan.ActorConstructor
	// even actors push, odd actors receive
	if isReceiver {
		constructor = newReceiver
	} else {
		constructor = newPusher
	}

	if err := p.ConstructActor(ctx, constructor); err != nil {
		return err
	}

	// Share this node's info w/ all nodes on the network
	if err := p.ShareInfo(ctx); err != nil {
		return err
	}

	var executeActions actorActions
	if isReceiver {
		executeActions = receiverActions
	} else {
		executeActions = pusherActions
	}
	if err := executeActions(ctx, p); err != nil {
		p.Runenv.RecordFailure(err)
	}
	return <-p.Finished(ctx)
}

var eventsToHandle = []event.Type{
	event.ETP2PQriPeerConnected,
	event.ETP2PPeerConnected,
}

// handles events for
// event.ETP2PQriPeerConnected
// event.ETP2PPeerConnected
func eventHandler(ctx context.Context, p *plan.Plan) event.Handler {
	return func(ctx context.Context, t event.Type, payload interface{}) error {
		switch t {
		case event.ETP2PQriPeerConnected:
			if pro, ok := payload.(*profile.Profile); ok {
				p.Runenv.RecordMessage("qri peer connected! %#v", pro)
				return nil
			}
			return fmt.Errorf("event.ETP2PQriPeerConnected payload not the expected *profile.Profile")
		case event.ETP2PPeerConnected:
			if pi, ok := payload.(peer.AddrInfo); ok {
				p.Runenv.RecordMessage("peer connected: %s", pi.String())
				return nil
			}
			return fmt.Errorf("event.ETP2PPeerConnected payload not the expected peer.AddrInfo")
		}
		return nil
	}
}

func getPushersPerReceiver(p *plan.Plan) int {
	ppr := p.Runenv.IntParam("pushersPerReceiver")
	if ppr < 1 {
		return defaultPushersPerReceiver
	}
	return ppr
}

func getDatasetSize(p *plan.Plan) int {
	datasetSize := p.Runenv.IntParam("datasetSize")
	if datasetSize < 1 {
		return defaultDatasetSize
	}
	return datasetSize
}

// remoteInfo contains the details needed for the pusher to connect to the
// remote. When we create a `newReceiver`, we broadcast the receiver's
// `remoteInfo` over the `rt` ("remote-info") topic when we create a
// `newPusher`, we subscribe to the `rt` topic to get each remote's info
type remoteInfo struct {
	Peername string // qri username
	PeerID   string // peerID associated with the remote
}

var rt = sync.NewTopic("remote-info", &remoteInfo{})
var remoteInfoSent = sync.State("remote info sent")

func newPusher(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
	act, err := sim.NewActor(ctx, p.Runenv, p.Client, p.Seq, lib.OptEventHandler(eventHandler(ctx, p), eventsToHandle...))
	if err != nil {
		return nil, err
	}

	if err := act.GenerateDatasetVersion(datasetName, getDatasetSize(p)); err != nil {
		return nil, err
	}

	if err := act.Inst.Connect(ctx); err != nil {
		return nil, err
	}

	// TODO (ramfox): this feels a bit redundant! didn't we just receive info??
	// well, until we know better what should belong in the `sim` package
	// and what should belong in the testcase specific package, let's leave
	// this here. Potentially, sim should allow the testcase to specify what it
	// is sharing when we `ShareInfo` and how we want to store it.
	receiversNum := p.Runenv.TestInstanceCount / (getPushersPerReceiver(p) + 1)
	p.Runenv.RecordMessage("waiting for remote info")
	<-p.Client.MustBarrier(ctx, remoteInfoSent, receiversNum).C

	rtCh := make(chan *remoteInfo)
	p.Client.Subscribe(ctx, rt, rtCh)

	act.Inst.Config().Remotes = &config.Remotes{}
	for i := 0; i < receiversNum; i++ {
		r := <-rtCh
		act.Inst.Config().Remotes.SetArbitrary(r.Peername, r.PeerID)
		p.Runenv.RecordMessage("received remote info from %q", r.Peername)
	}

	p.Runenv.RecordMessage("I'm a Pusher named %s", act.Peername())
	p.Runenv.RecordMessage("My qri ID is %s", act.ID())
	p.Runenv.RecordMessage("My peer ID is %s", act.AddrInfo().ID)
	p.Runenv.RecordMessage("My remotes are %v", act.Inst.Config().Remotes)
	return act, err
}

func newReceiver(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
	opts := []lib.Option{
		lib.OptEnableRemote(),
		lib.OptEventHandler(eventHandler(ctx, p), eventsToHandle...),
	}

	act, err := sim.NewActor(ctx, p.Runenv, p.Client, p.Seq, opts...)
	if err != nil {
		return nil, err
	}

	if err := act.Inst.Connect(ctx); err != nil {
		return nil, err
	}

	p.Runenv.RecordMessage("I'm a Receiver named %s", act.Peername())
	p.Runenv.RecordMessage("My qri ID is %s", act.ID())
	p.Runenv.RecordMessage("My peer ID is %s", act.AddrInfo().ID)

	pro, err := act.Inst.Repo().Profile()
	if err != nil {
		return nil, err
	}

	// TODO (ramfox): this feels a bit redundant! didn't we just send info??
	// well, until we know better what should belong in the `sim` package
	// and what should belong in the testcase specific package, let's leave
	// this here. Potentially, sim should allow the testcase to specify what it
	// is sharing when we `ShareInfo` and how we want to store it.
	p.Runenv.RecordMessage("Sending my remote info")
	p.Client.Publish(ctx, rt, &remoteInfo{
		Peername: pro.Peername,
		PeerID:   act.AddrInfo().ID.String(),
	})
	p.Client.MustSignalEntry(ctx, remoteInfoSent)

	return act, err
}

func pushToAllRemotes(ctx context.Context, p *plan.Plan) error {
	remotes := p.Actor.Inst.Config().Remotes
	if remotes == nil {
		return fmt.Errorf("This actor does not know of any remotes, are you sure it is a pusher?")
	}

	// create remote methods
	rm := lib.NewRemoteMethods(p.Actor.Inst)
	var accErr error

	// iterate over each remote and attempt to push to each
	for name := range *remotes {
		pp := &lib.PublicationParams{
			Ref:        fmt.Sprintf("%s/%s", p.Actor.Peername(), datasetName),
			RemoteName: name,
			All:        true,
		}
		ref := &dsref.Ref{}
		if err := rm.Publish(pp, ref); err != nil {
			accErr = accumulateErrors(accErr, fmt.Errorf("error pushing %q to %q: %s", pp.Ref, pp.RemoteName, err))
		}
	}
	// signal a push attempt has been made
	p.Client.MustSignalEntry(ctx, sim.StatePushAttempted)
	p.Runenv.RecordMessage("pushed to all remotes")
	return accErr
}

func accumulateErrors(errors, newError error) error {
	if errors == nil {
		return newError
	}
	return fmt.Errorf("%s\n%s", errors.Error(), newError.Error())
}

// actorActions are the actions an actor should take during the test, specific
// to the kind of actor it is, aka pusher or receiver
type actorActions func(context.Context, *plan.Plan) error

// pusherActions execute the actions that the pusher should take:
// - announce it is about to push
// - push a dataset to all remotes on the remote list
func pusherActions(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("About to push to remote")
	if err := pushToAllRemotes(ctx, p); err != nil {
		return err
	}
	p.ActorFinished(ctx)
	return nil
}

// receiverActions execute the actions that the receiver should take:
// - announce it is waiting for dataset
// - wait until the expected number of datasets have attempted to be sent
// - announce we are finished waiting
// - list all logs in its logbook
func receiverActions(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("Waiting for dataset")
	numOfPushers := p.Runenv.TestInstanceCount - (p.Runenv.TestGroupInstanceCount / (getPushersPerReceiver(p) + 1))
	<-p.Client.MustBarrier(ctx, sim.StatePushAttempted, numOfPushers).C

	p.Runenv.RecordMessage("Finished waiting")
	p.Runenv.RecordMessage("Listing all foreign logs:")
	logs, err := p.Actor.Inst.Repo().Logbook().ListAllLogs(ctx)
	if err != nil {
		return fmt.Errorf("error listing all logs: %s", err)
	}
	for _, log := range logs {
		if log.Name() == p.Actor.Peername() {
			continue
		}
		ref := fmt.Sprintf("%s@%s", log.Name(), log.Author())
		for _, l := range log.Logs {
			p.Runenv.RecordMessage("   %s/%s", ref, l.Name())
		}
	}
	p.ActorFinished(ctx)
	return nil
}
