package main

import (
	"context"
	"fmt"
	"time"

	"github.com/qri-io/qri/event"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/qri/repo/profile"
	"github.com/qri-io/test-plans/plan"
	"github.com/qri-io/test-plans/sim"
	"github.com/testground/sdk-go/sync"
)

var doneRecievingProfiles = sync.State("done receiving profiles")

// RunPlanProfileService creates an instance, connects to each instance, waits
// for the profile exchange to finish, and lists all the known profiles
func RunPlanProfileService(ctx context.Context, p *plan.Plan) error {
	var (
		qriPeerConnCh     = make(chan profile.ID)
		connectedQriPeers = []profile.ID{}
		profileWait       = make(chan struct{})
		profileServiceCtx context.Context
	)
	defer func() {
		close(qriPeerConnCh)
		close(profileWait)
	}()

	if err := p.SetupNetwork(ctx); err != nil {
		return err
	}

	timeout := p.Runenv.IntParam("profile_service_timeout_sec")
	profileServiceCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	go func() {
		for {
			select {
			case ProfileService := <-qriPeerConnCh:
				ok := false
				for _, valProfileService := range connectedQriPeers {
					if valProfileService == ProfileService {
						ok = true
					}
				}
				if !ok {
					connectedQriPeers = append(connectedQriPeers, ProfileService)
					if len(connectedQriPeers) == p.Runenv.TestGroupInstanceCount-1 {
						profileWait <- struct{}{}
						return
					}
				}
			case <-profileServiceCtx.Done():
				p.Runenv.RecordFailure(fmt.Errorf("context timed out before all profiles were recieved"))
				profileWait <- struct{}{}
				return
			}
		}
	}()

	if err := p.ConstructActor(ctx, newConnector(qriPeerConnCh)); err != nil {
		return err
	}

	if err := p.ShareInfo(ctx); err != nil {
		return err
	}

	if _, err := p.DialOtherPeers(ctx); err != nil {
		p.Runenv.RecordFailure(err)
	}

	p.Runenv.RecordMessage("waiting to connect to all qri nodes")
	<-profileWait
	if err := listAllKnownProfiles(ctx, p); err != nil {
		p.Runenv.RecordFailure(err)
	}

	p.Runenv.RecordMessage("waiting for all qri nodes to be finished exchanging profiles")
	p.Client.MustSignalEntry(ctx, doneRecievingProfiles)
	sendAttempts := p.Runenv.TestInstanceCount
	<-p.Client.MustBarrier(ctx, doneRecievingProfiles, sendAttempts).C

	return <-p.Finished(ctx)
}

func profileServiceEventHandler(ctx context.Context, p *plan.Plan, qriPeerConnCh chan profile.ID) event.Handler {
	return func(ctx context.Context, t event.Type, payload interface{}) error {
		pro, ok := payload.(*profile.Profile)
		if !ok {
			err := fmt.Errorf("unexpected event payload, expected type *profile.Profile")
			p.Runenv.RecordFailure(err)
			return err
		}
		switch t {
		case event.ETP2PQriPeerConnected:
			p.Runenv.RecordMessage("Profile exchange request received from %q", pro.Peername)
			qriPeerConnCh <- pro.ID
			return nil
		default:
			err := fmt.Errorf("unexpected event type: %s", t)
			p.Runenv.RecordFailure(err)
			return err
		}
	}
}

var profileServiceEventsToHandle = []event.Type{
	event.ETP2PQriPeerConnected,
}

func newConnector(qriPeerConnCh chan profile.ID) plan.ActorConstructor {
	return func(ctx context.Context, p *plan.Plan) (*sim.Actor, error) {
		act, err := sim.NewActor(ctx, p.Runenv, p.Client, p.Seq, lib.OptEventHandler(profileServiceEventHandler(ctx, p, qriPeerConnCh), profileServiceEventsToHandle...))
		if err != nil {
			return nil, err
		}

		if err := act.Inst.Connect(ctx); err != nil {
			return nil, err
		}

		p.Client.MustSignalEntry(ctx, sim.StateActorConstructed)
		p.Runenv.RecordMessage("\nI'm a Connector named %s\nMy qri ID is %s\nMy peer ID is %s\nMy addrs are %s", act.Peername(), act.ID(), act.AddrInfo().ID, act.AddrInfo().Addrs)

		<-p.Client.MustBarrier(ctx, sim.StateActorConstructed, p.Runenv.TestInstanceCount).C
		return act, err
	}
}

func listAllKnownProfiles(ctx context.Context, p *plan.Plan) error {
	profileList, err := p.Actor.Inst.Repo().Profiles().List()
	if err != nil {
		p.Runenv.RecordFailure(fmt.Errorf("unable to list profiles: %s", err))
		return err
	}
	msg := "\nlisting profiles for known instances: "
	for id, profile := range profileList {
		msg += fmt.Sprintf("\n  %s, %s", profile.Peername, id)
	}
	p.Runenv.RecordMessage(msg)
	p.ActorFinished(ctx)
	return nil
}
