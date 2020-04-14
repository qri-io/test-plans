package sim

import (
	"context"
	"fmt"

	"github.com/qri-io/qri/lib"
	"github.com/qri-io/qri/remote"
	"github.com/qri-io/qri/repo/profile"
	reporef "github.com/qri-io/qri/repo/ref"

	"github.com/ipfs/testground/sdk/runtime"
)

var (
	// ErrAlreadyPushed indicates a dataset has already been published
	// TODO (b5) - move this into lib/remote
	ErrAlreadyPushed = fmt.Errorf("this dataset has already been published")
)

// RemoteHooks implements remote behaviour for cloud backend to store and pin
// datasets. Remote invokes cloud infrastructure on successful push
type RemoteHooks struct {
	runenv *runtime.RunEnv
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
	})
}

func (r *RemoteHooks) acceptPushPreCheck(ctx context.Context, pid profile.ID, ref reporef.DatasetRef) error {
	return nil
}

func (r *RemoteHooks) acceptPushFinalCheck(ctx context.Context, pid profile.ID, ref reporef.DatasetRef) error {
	return nil
}

func (r *RemoteHooks) datasetPushed(ctx context.Context, pid profile.ID, ref reporef.DatasetRef) error {
	r.runenv.Message("RemoteHooks.datasetPushed pid %s ref %s", pid, ref.String())
	// def := runtime.MetricDefinition{
	// 	Name:           "donkeypower",
	// 	Unit:           "kiloforce",
	// 	ImprovementDir: -1,
	// }
	// r.runenv.EmitMetric(&def, 3.0)

	return nil
}

func (r *RemoteHooks) datasetPullPreCheck(ctx context.Context, pid profile.ID, ref reporef.DatasetRef) error {

	return nil
}

func (r *RemoteHooks) datasetPulled(ctx context.Context, pid profile.ID, ref reporef.DatasetRef) error {
	r.runenv.Message("RemoteHooks.datasetPulled: %s", ref.String())

	return nil
}

func (r *RemoteHooks) datasetRemoved(ctx context.Context, pid profile.ID, ref reporef.DatasetRef) error {
	// log.Debug("RemoteHooks.datasetRemoved")
	return nil
}
