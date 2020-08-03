package main

import (
	"context"
	"fmt"

	peer "github.com/libp2p/go-libp2p-peer"
	"github.com/qri-io/qri/event"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/test-plans/plan"
	"github.com/qri-io/test-plans/sim"
)

// RunPlanProfileExchange creates an instance, connects to each instance, waits
// for the profile exchange to finish, and lists all the known profiles
func RunPlanProfileExchange(ctx context.Context, p *plan.Plan) error {
	if err := p.SetupNetwork(ctx); err != nil {
		return err
	}

	if err := p.ConstructActor(ctx, newConnector); err != nil {
		return err
	}

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

	// wait until we have attempted to exchange profiles with all the instances
	// with which we have connectedc
	p.Wg.Wait()

	if err := listAllKnownProfiles(p); err != nil {
		p.Runenv.RecordFailure(err)
	}
	return <-p.Finished(ctx)
}

func profileExchangeEventHandler(ctx context.Context, p *plan.Plan) event.Handler {
	return func(ctx context.Context, t event.Type, payload interface{}) error {
		id, ok := payload.(peer.ID)
		if !ok {
			err := fmt.Errorf("unexpected event payload, expected type peer.ID")
			p.Runenv.RecordFailure(err)
			return err
		}
		switch t {
		case event.ETP2PProfileExchangeRequestRecieved:
		case event.ETP2PProfileExchangeRequestSent:
			action := "received from"
			if t == event.ETP2PProfileExchangeRequestSent {
				action = "sent to"
			}
			p.Wg.Add(1)
			p.Runenv.RecordMessage("Profile exchange request %s %q", action, id)
			return nil
		case event.ETP2PProfileExchangeSuccess:
		case event.ETP2PProfileExchangeFailed:
			status := "success"
			if t == event.ETP2PProfileExchangeFailed {
				status = "failed"
			}
			p.Runenv.RecordMessage("Profile exchange with %q %s", id, status)
			// once we have successfully or unsuccessfully exchanged profiles
			// make sure we remove one from the wait group
			p.Wg.Done()
			return nil
		default:
			err := fmt.Errorf("unexpected event type: %s", t)
			p.Runenv.RecordFailure(err)
			return err
		}
		return nil
	}
}

var profileExchangeEventsToHandle = []event.Type{
	event.ETP2PProfileExchangeRequestRecieved,
	event.ETP2PProfileExchangeRequestSent,
	event.ETP2PProfileExchangeSuccess,
	event.ETP2PProfileExchangeFailed,
}

func newConnector(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
	act, err := sim.NewActor(ctx, p.Runenv, p.Client, p.Seq, lib.OptEventHandler(profileExchangeEventHandler(ctx, p), profileExchangeEventsToHandle...))
	if err != nil {
		return nil, err
	}

	if err := act.Inst.Node().GoOnline(ctx); err != nil {
		return nil, err
	}

	p.Runenv.RecordMessage("I'm a Connector named %s", act.Peername())
	p.Runenv.RecordMessage("My qri ID is %s", act.ID())
	p.Runenv.RecordMessage("My peer ID is %s", act.AddrInfo().ID)
	return act, err
}

func connectToInstances(ctx context.Context, p *plan.Plan) error {
	var accErr error
	for _, info := range p.Others {
		if err := p.Actor.Inst.Node().Host().Connect(ctx, *info.AddrInfo); err != nil {
			accErr = accumulateErrors(accErr, fmt.Errorf("error connecting to %q aka %q", info.AddrInfo.ID, info.Peername))
		}
	}
	// signal a pull attempt has been made
	p.Client.MustSignalEntry(ctx, sim.StateConnectionAttempted)
	p.Runenv.RecordMessage("attempted to connect to all instances")
	waitForConnections(ctx, p)
	return accErr
}

func waitForConnections(ctx context.Context, p *plan.Plan) error {
	p.Runenv.RecordMessage("Waiting for other instances to finish connecting")
	<-p.Client.MustBarrier(ctx, sim.StateConnectionAttempted, p.Runenv.TestInstanceCount/2).C
	return nil
}

func listAllKnownProfiles(p *plan.Plan) error {
	p.Runenv.RecordMessage("listing profiles for known instances:")
	profileList, err := p.Actor.Inst.Repo().Profiles().List()
	if err != nil {
		p.Runenv.RecordFailure(fmt.Errorf("unable to list profiles: %s", err))
		return err
	}
	for id, profile := range profileList {
		p.Runenv.RecordMessage("  %s, %s", profile.Peername, id)
	}
	return nil
}
