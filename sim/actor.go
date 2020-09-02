package sim

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/qri-io/dataset"
	"github.com/qri-io/dataset/dsio"
	"github.com/qri-io/dataset/generate"
	"github.com/qri-io/ioes"
	"github.com/qri-io/qfs"
	"github.com/qri-io/qri/config"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/qri/repo/gen"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var (
	qriRepoPath string
	// StateActorConstructed is the state to sync on to check if all the
	// actors in the test have been constructed
	StateActorConstructed = sync.State("actor has been constructed")
)

func init() {
	tempDir, _ := ioutil.TempDir("", "remote_test_path")
	qriRepoPath = filepath.Join(tempDir, "qri")
}

// Actor is a peer in a network simulation
type Actor struct {
	Inst  *lib.Instance
	hooks *RemoteHooks
}

// NewActor creates an actor instance
func NewActor(ctx context.Context, runenv *runtime.RunEnv, client sync.Client, seq int64, opts ...lib.Option) (*Actor, error) {
	var listeningAddrs []string

	netClient := network.NewClient(client, runenv)
	if ip := netClient.MustGetDataNetworkIP(); ip.String() != "127.0.0.1" {
		listeningAddrs = []string{fmt.Sprintf("/ip4/%s/tcp/0", ip)}
	}

	if err := setup(defaultQriActorConfig(listeningAddrs)); err != nil {
		return nil, err
	}

	hooks := &RemoteHooks{runenv: runenv, client: client}

	libOpts := []lib.Option{
		lib.OptIOStreams(ioes.NewStdIOStreams()),
		hooks.RemoteOptionsFunc(),
		lib.OptNoBootstrap(),
	}
	for _, opt := range opts {
		libOpts = append(libOpts, opt)
	}

	inst, err := lib.NewInstance(ctx, qriRepoPath, libOpts...)
	if err != nil {
		return nil, err
	}

	act := &Actor{
		Inst:  inst,
		hooks: hooks,
	}

	return act, nil
}

// setup initializes on-disk IPFS & qri repos, generates private keys
func setup(cfg *config.Config) error {
	p := lib.SetupParams{
		SetupIPFS: true,
		Register:  false,
		Config:    cfg,
		Generator: gen.NewCryptoSource(),
		RepoPath:  qriRepoPath,
	}

	return lib.Setup(p)
}

// ActorInfo carries details about an actor
type ActorInfo struct {
	Seq       int // sequence number within the test
	Peername  string
	ProfileID string
	AddrInfo  *peer.AddrInfo
}

// Info returns details about this actor
func (a *Actor) Info(runenv *runtime.RunEnv) *ActorInfo {
	pro, _ := a.Inst.Repo().Profile()
	return &ActorInfo{
		Seq:       runenv.TestInstanceCount,
		Peername:  pro.Peername,
		ProfileID: pro.ID.String(),
		AddrInfo:  a.AddrInfo(),
	}
}

// Peername returns this actor's peername
func (a *Actor) Peername() string {
	pro, _ := a.Inst.Repo().Profile()
	return pro.Peername
}

// ID provides a string identifier for this peer
// TODO (b5) - this should be a logbook identifier
func (a *Actor) ID() string {
	pro, _ := a.Inst.Repo().Profile()
	return pro.ID.String()
}

// AddrInfo provides this peers address information
func (a *Actor) AddrInfo() *peer.AddrInfo {
	h := a.Inst.Node().Host()
	return &peer.AddrInfo{
		ID:    h.ID(),
		Addrs: h.Addrs(),
	}
}

// GenerateDatasetVersion creates & Saves a new version of a dataset
// Datasets are generic CSV datasets with only the number of rows configurable
// We're trying to test the network here. Size should be the only real concern
func (a *Actor) GenerateDatasetVersion(name string, numRows int) error {
	csvFilepath, err := generateRandomCSVFile(numRows)
	if err != nil {
		return err
	}

	p := &lib.SaveParams{
		Ref:        fmt.Sprintf("me/%s", name),
		BodyPath:   csvFilepath,
		UseDscache: true,
	}

	ds := &dataset.Dataset{}
	return lib.NewDatasetMethods(a.Inst).Save(p, ds)
}

// // MarkDatasetAsPublished
// func (a *Actor)

func generateRandomCSVFile(numRows int) (string, error) {
	st := &dataset.Structure{
		Format: "csv",
		FormatConfig: map[string]interface{}{
			"headerRow": true,
		},
		Schema: map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "array",
				"items": []interface{}{
					map[string]interface{}{"title": "id", "type": "string"},
					map[string]interface{}{"title": "date", "type": "string"},
					map[string]interface{}{"title": "count", "type": "integer"},
					map[string]interface{}{"title": "data", "type": "string"},
				},
			},
		},
	}
	gen, err := generate.NewTabularGenerator(st)
	if err != nil {
		return "", err
	}

	f, err := ioutil.TempFile("", "body.*.csv")
	if err != nil {
		return "", err
	}

	w, err := dsio.NewCSVWriter(st, f)
	if err != nil {
		return "", err
	}

	for i := 0; i < numRows; i++ {
		ent, err := gen.ReadEntry()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		w.WriteEntry(ent)
	}
	w.Close()
	gen.Close()

	return f.Name(), nil
}

func defaultQriActorConfig(listeningAddrs []string) *config.Config {
	return &config.Config{
		Profile: &config.ProfilePod{
			Type:    "peer",
			Color:   "",
			Created: time.Now(),
		},
		Filesystems: []qfs.Config{
			{
				Type: "ipfs",
				Config: map[string]interface{}{
					"path":           filepath.Join(qriRepoPath, "ipfs"),
					"listeningAddrs": listeningAddrs,
				},
			},
			{Type: "local"},
			{Type: "http"},
		},
		Repo: &config.Repo{
			Type: "fs",
		},
		API: &config.API{
			AllowedOrigins: []string{},
		},
		P2P: &config.P2P{
			Enabled:           true,
			QriBootstrapAddrs: []string{},
		},
		Remote: &config.Remote{
			Enabled:       true,
			AcceptSizeMax: -1,
			// RequireAllBlocks: true,
			AllowRemoves: true,
		},
		Logging: &config.Logging{
			Levels: map[string]string{
				"event": "debug",
				"p2p":   "debug",
			},
		},
		CLI: &config.CLI{},
		RPC: &config.RPC{},
		Registry: &config.Registry{
			Location: "",
		},
	}
}
