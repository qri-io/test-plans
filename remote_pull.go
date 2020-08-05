package main

import (
	"context"
	"fmt"

	peer "github.com/libp2p/go-libp2p-peer"
	"github.com/qri-io/dataset"
	"github.com/qri-io/qri/config"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/test-plans/plan"
	"github.com/qri-io/test-plans/sim"
)

var pullDatasetName = "megajoules"
var defaultPullDatasetSize = 1000
var defaultPullersPerRemote = 1

// RunPlanRemotePull demonstrates test output functions
// This method emits two Messages and one Metric
func RunPlanRemotePull(ctx context.Context, p *plan.Plan) error {
	pullersPerRemote := getPullersPerRemote(p)
	if pullersPerRemote >= p.Runenv.TestInstanceCount {
		return fmt.Errorf("Pull variable specify %d puller per receiver, but there are only %d instances", pullersPerRemote, p.Runenv.TestInstanceCount)
	}
	if err := p.SetupNetwork(ctx); err != nil {
		return err
	}

	isRemote := p.Seq%(int64(pullersPerRemote+1)) == 0

	var constructor plan.ActorConstructor
	// even actors pull, odd actors receive
	if isRemote {
		constructor = newRemote
	} else {
		constructor = newPuller
	}

	if err := p.ConstructActor(ctx, constructor); err != nil {
		return err
	}

	// Share this node's info w/ all nodes on the network
	if err := p.ShareInfo(ctx); err != nil {
		return err
	}

	var executeActions actorActions
	if isRemote {
		executeActions = remoteActions
	} else {
		executeActions = pullerActions
	}
	if err := executeActions(ctx, p); err != nil {
		p.Runenv.RecordFailure(err)
	}

	return <-p.Finished(ctx)
}

func getPullersPerRemote(p *plan.Plan) int {
	ppr := p.Runenv.IntParam("pullersPerRemote")
	if ppr < 1 {
		return defaultPullersPerRemote
	}
	return ppr
}

func newPuller(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
	act, err := sim.NewActor(ctx, p.Runenv, p.Client, p.Seq, lib.OptEventHandler(eventHandler(ctx, p), eventsToHandle...))
	if err != nil {
		return nil, err
	}

	if err := act.GenerateDatasetVersion(pullDatasetName, getDatasetSize(p)); err != nil {
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
	receiversNum := p.Runenv.TestInstanceCount / (getPullersPerRemote(p) + 1)
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

	p.Runenv.RecordMessage("I'm a Puller named %s", act.Peername())
	p.Runenv.RecordMessage("My qri ID is %s", act.ID())
	p.Runenv.RecordMessage("My peer ID is %s", act.AddrInfo().ID)
	p.Runenv.RecordMessage("My remotes are %v", act.Inst.Config().Remotes)
	return act, err
}

func newRemote(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
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

	p.Runenv.RecordMessage("I'm a Remote named %s", act.Peername())
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
	// id := act.AddrInfo().ID.String()
	// p.Runenv.RecordMessage("id = %q", id)
	// if _, err := peer.IDFromString(id); err != nil {
	// 	p.Runenv.RecordMessage("cannot parse id after stringifying!")
	// }
	p.Client.Publish(ctx, rt, &remoteInfo{
		Peername: pro.Peername,
		PeerID:   act.AddrInfo().ID.Pretty(),
	})
	p.Client.MustSignalEntry(ctx, remoteInfoSent)

	return act, err
}

func pullFromAllRemotes(ctx context.Context, p *plan.Plan) error {
	remotes := p.Actor.Inst.Config().Remotes
	if remotes == nil {
		return fmt.Errorf("This actor does not know of any remotes, are you sure it is a puller?")
	}

	// create remote methods
	dm := lib.NewDatasetMethods(p.Actor.Inst)
	var accErr error

	// iterate over each remote and attempt to pull to sssseach
	for name, stringID := range *remotes {
		id, err := peer.IDB58Decode(stringID)
		if err != nil {
			accErr = accumulateErrors(accErr, fmt.Errorf("error parsing remote %q peer id %q: %s", name, stringID, err))
			continue
		}
		remoteAddrs := p.Actor.Inst.Node().Host().Peerstore().Addrs(id)
		for _, addr := range remoteAddrs {
			p.Runenv.RecordMessage("remote addr: %s", addr)
		}
		if len(remoteAddrs) < 1 {
			accErr = accumulateErrors(accErr, fmt.Errorf("remote %s has no addrs", name))
			continue
		}
		pp := &lib.PullParams{
			Ref:        fmt.Sprintf("%s/%s", name, pullDatasetName),
			RemoteAddr: remoteAddrs[0].String(),
		}
		ds := &dataset.Dataset{}
		if err := dm.Pull(pp, ds); err != nil {
			accErr = accumulateErrors(accErr, fmt.Errorf("error pulling %q to %q: %s", pp.Ref, name, err))
		}
	}
	// signal a pull attempt has been made
	p.Client.MustSignalEntry(ctx, sim.StatePullAttempted)
	p.Runenv.RecordMessage("pulled to all remotes")
	return accErr
}

// pullerActions execute the actions that the puller should take:
// - announce it is about to pull
// - pull a dataset from all remotes on the remote list
// - announce it is finished pulling
func pullerActions(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("About to pull from remotes")
	if err := pullFromAllRemotes(ctx, p); err != nil {
		return err
	}
	p.Runenv.RecordMessage("Finished pulling")
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

// remoteActions execute the actions that the receiver should take:
// - announce it is waiting for dataset pulls
// - wait until all pulls have happened
// - announce closing
func remoteActions(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("Waiting for dataset pulls")
	numOfPullers := p.Runenv.TestInstanceCount - (p.Runenv.TestGroupInstanceCount / (getPullersPerRemote(p) + 1))
	<-p.Client.MustBarrier(ctx, sim.StatePushAttempted, numOfPullers).C

	p.Runenv.RecordMessage("Finished waiting")
	p.ActorFinished(ctx)
	return nil

}
