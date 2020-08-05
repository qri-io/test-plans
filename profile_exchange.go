package main

import (
	"context"
	"fmt"

	peer "github.com/libp2p/go-libp2p-peer"
	"github.com/qri-io/qri/event"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/test-plans/plan"
	"github.com/qri-io/test-plans/sim"
	"github.com/testground/sdk-go/sync"
)

var profileSendAttempted = sync.State("attempted to send profile")

// RunPlanProfile creates an instance, connects to each instance, waits
// for the profile exchange to finish, and lists all the known profiles
func RunPlanProfile(ctx context.Context, p *plan.Plan) error {
	if err := p.SetupNetwork(ctx); err != nil {
		return err
	}

	if err := p.ConstructActor(ctx, newConnector); err != nil {
		return err
	}

	<-p.Client.MustBarrier(ctx, sim.StateActorConstructed, p.Runenv.TestInstanceCount).C

	// Share this node's info w/ all nodes on the network
	if err := p.ShareInfo(ctx); err != nil {
		return err
	}

	isConnector := true
	if p.Seq%2 == 0 {
		isConnector = false
	}

	var executeActions actorActions
	if isConnector {
		executeActions = connectToInstances
	} else {
		executeActions = waitForConnections
	}

	if err := executeActions(ctx, p); err != nil {
		p.Runenv.RecordFailure(err)
	}

	// if err := connectToInstances(ctx, p); err != nil {
	// 	p.Runenv.RecordFailure(err)
	// }

	// how to get the test to wait until all connections have finished?
	// everyone has had a sent failure or success for each other node?
	// that seems excessive but w.e

	p.Runenv.RecordMessage("waiting for all connections to send profiles")
	// this is a stand in for now, need to think of a better way to barrier
	// each node sends to each other node
	sendAttempts := p.Runenv.TestInstanceCount * (p.Runenv.TestInstanceCount - 1)
	<-p.Client.MustBarrier(ctx, profileSendAttempted, sendAttempts).C

	if err := listAllKnownProfiles(ctx, p); err != nil {
		p.Runenv.RecordFailure(err)
	}
	return <-p.Finished(ctx)
}

func profileEventHandler(ctx context.Context, p *plan.Plan) event.Handler {
	return func(ctx context.Context, t event.Type, payload interface{}) error {
		id, ok := payload.(peer.ID)
		if !ok {
			err := fmt.Errorf("unexpected event payload, expected type peer.ID")
			p.Runenv.RecordFailure(err)
			return err
		}
		switch t {
		case event.ETP2PProfileRequestReceived:
			p.Wg.Add(1)
			p.Runenv.RecordMessage("Profile exchange request received from %q", id)
			return nil
		case event.ETP2PProfileRequestSent:
			p.Wg.Add(1)
			p.Runenv.RecordMessage("Profile exchange request sent to %q", id)
			return nil
		case event.ETP2PProfileSendSuccess:
			p.Runenv.RecordMessage("Profile sent to %q successfully!", id)
			p.Wg.Done()
			// sync
			p.Client.MustSignalEntry(ctx, profileSendAttempted)
			return nil
		case event.ETP2PProfileSendFailed:
			p.Runenv.RecordMessage("Profile send to %q failed", id)
			p.Wg.Done()
			p.Client.MustSignalEntry(ctx, profileSendAttempted)
			return nil
		case event.ETP2PProfileReceiveSuccess:
			p.Runenv.RecordMessage("Profile received from %q successfully!", id)
			p.Wg.Done()
			return nil
		case event.ETP2PProfileReceiveFailed:
			p.Runenv.RecordMessage("Profile receive to %q failed", id)
			p.Wg.Done()
			return nil
		default:
			err := fmt.Errorf("unexpected event type: %s", t)
			p.Runenv.RecordFailure(err)
			return err
		}
	}
}

var profileEventsToHandle = []event.Type{
	event.ETP2PProfileRequestReceived,
	event.ETP2PProfileRequestSent,
	event.ETP2PProfileSendSuccess,
	event.ETP2PProfileSendFailed,
	event.ETP2PProfileReceiveSuccess,
	event.ETP2PProfileReceiveFailed,
}

func newConnector(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
	act, err := sim.NewActor(ctx, p.Runenv, p.Client, p.Seq, lib.OptEventHandler(profileEventHandler(ctx, p), profileEventsToHandle...))
	if err != nil {
		return nil, err
	}

	if err := act.Inst.Connect(ctx); err != nil {
		return nil, err
	}

	p.Client.MustSignalEntry(ctx, sim.StateActorConstructed)
	p.Runenv.RecordMessage("I'm a Connector named %s", act.Peername())
	p.Runenv.RecordMessage("My qri ID is %s", act.ID())
	p.Runenv.RecordMessage("My peer ID is %s", act.AddrInfo().ID)
	p.Runenv.RecordMessage("My addrs are %s", act.AddrInfo().Addrs)
	return act, err
}

func connectToInstances(ctx context.Context, p *plan.Plan) error {
	var accErr error
	for _, info := range p.Others {
		_, err := p.Actor.Inst.Node().Host().NewStream(ctx, info.AddrInfo.ID)
		if err != nil {
			accErr = accumulateErrors(accErr, fmt.Errorf("error connecting to %q aka %q: %s", info.AddrInfo.ID, info.Peername, err))
		}
		// if err := p.Actor.Inst.Node().Host().Connect(ctx, *info.AddrInfo); err != nil {
		// 	accErr = accumulateErrors(accErr, fmt.Errorf("error connecting to %q aka %q: %s", info.AddrInfo.ID, info.Peername, err))
		// }
	}
	// signal a pull attempt has been made
	p.Client.MustSignalEntry(ctx, sim.StateConnectionAttempted)
	p.Runenv.RecordMessage("attempted to connect to all instances")
	waitForConnections(ctx, p)
	return accErr
}

func requestProfile(ctx context.Context, p *plan.Plan) error {
	var accErr error
	for _, info := range p.Others {
		if err := p.Actor.Inst.Node().Host().Connect(ctx, *info.AddrInfo); err != nil {
			accErr = accumulateErrors(accErr, fmt.Errorf("error connecting to %q aka %q: %s", info.AddrInfo.ID, info.Peername, err))
		}
	}
	// // signal a pull attempt has been made
	// p.Client.MustSignalEntry(ctx, sim.StateConnectionAttempted)
	// p.Runenv.RecordMessage("attempted to connect to all instances")
	// waitForConnections(ctx, p)
	return accErr
}

func waitForConnections(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("Waiting for other instances to finish connecting")
	<-p.Client.MustBarrier(ctx, sim.StateConnectionAttempted, p.Runenv.TestInstanceCount/2).C
	return nil
}

func listAllKnownProfiles(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("listing profiles for known instances:")
	profileList, err := p.Actor.Inst.Repo().Profiles().List()
	if err != nil {
		p.Runenv.RecordFailure(fmt.Errorf("unable to list profiles: %s", err))
		return err
	}
	for id, profile := range profileList {
		p.Runenv.RecordMessage("  %s, %s", profile.Peername, id)
	}
	p.ActorFinished(ctx)
	return nil
}
