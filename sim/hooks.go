package sim

import (
	"context"
	"fmt"

	"github.com/qri-io/qri/dsref"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/qri/remote"
	"github.com/qri-io/qri/repo/profile"

	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var (
	// ErrAlreadyPushed indicates a dataset has already been published
	// TODO (b5) - move this into lib/remote
	ErrAlreadyPushed = fmt.Errorf("this dataset has already been published")
	// StatePushAttempted is the state to sync on once a pusher has tried to
	// push its dataset to all remotes, regardless of if the attempt was
	// successful
	StatePushAttempted = sync.State("push to all remotes attempted")
	// StatePullAttempted is the state to sync on once a puller has tried
	// pull a dataset from each remote, regardless of if the attempt was
	// successful
	StatePullAttempted = sync.State("pull from all remotes attempted")
	// StateConnectionAttempted is the state to sync on once an instance has attempted
	// to connect to each other instance
	StateConnectionAttempted = sync.State("connection to all other nodes attempted")
)

// RemoteHooks implements remote behaviour for cloud backend to store and pin
// datasets. Remote invokes cloud infrastructure on successful push
type RemoteHooks struct {
	runenv *runtime.RunEnv
	client sync.Client
}

// RemoteOptionsFunc creates a function to connect hooks to a remote at
// initialization
func (r *RemoteHooks) RemoteOptionsFunc() lib.Option {
	return lib.OptRemoteOptions(func(opts *remote.Options) {
		opts.DatasetPushPreCheck = r.acceptPushPreCheck
		opts.DatasetPushFinalCheck = r.acceptPushFinalCheck
		opts.DatasetPushed = r.datasetPushed
		opts.DatasetPullPreCheck = r.datasetPullPreCheck
		opts.DatasetPulled = r.datasetPulled
		opts.DatasetRemoved = r.datasetRemoved
		opts.LogPushPreCheck = r.logPushPreCheck
		opts.LogPushFinalCheck = r.logPushFinalCheck
		opts.LogPushed = r.logPushed
	})
}

func (r *RemoteHooks) acceptPushPreCheck(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("received push of dataset %q from %q", ref, pid)
	return nil
}

func (r *RemoteHooks) acceptPushFinalCheck(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("dataset %q from %q to start sending", ref, pid)
	return nil
}

func (r *RemoteHooks) datasetPushed(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("Success! Received dataset %q from %q", ref, pid)
	r.client.MustSignalEntry(ctx, StatePushAttempted)
	return nil
}

func (r *RemoteHooks) datasetPullPreCheck(ctx context.Context, pid profile.ID, ref dsref.Ref) error {

	return nil
}

func (r *RemoteHooks) datasetPulled(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("RemoteHooks.datasetPulled: %s", ref.String())

	return nil
}

func (r *RemoteHooks) datasetRemoved(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	// log.Debug("RemoteHooks.datasetRemoved")
	return nil
}

func (r *RemoteHooks) logPushPreCheck(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("received log push: %s", ref.String())

	return nil
}

func (r *RemoteHooks) logPushFinalCheck(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("log push final check: %s", ref.String())

	return nil
}

func (r *RemoteHooks) logPushed(ctx context.Context, pid profile.ID, ref dsref.Ref) error {
	r.runenv.RecordMessage("Success!!! RemoteHooks.logPushed: %s", ref.String())

	return nil
}
