package sim

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/qri-io/dataset"
	"github.com/qri-io/dataset/dsio"
	"github.com/qri-io/dataset/generate"
	"github.com/qri-io/ioes"
	"github.com/qri-io/qri/config"
	"github.com/qri-io/qri/event"
	"github.com/qri-io/qri/lib"
	"github.com/qri-io/qri/repo/gen"
	reporef "github.com/qri-io/qri/repo/ref"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/testground/sdk-go/runtime"
)

var (
	qriRepoPath, ipfsRepoPath string
)

func init() {
	qriRepoPath, _ = ioutil.TempDir("", "qri")
	ipfsRepoPath, _ = ioutil.TempDir("", "ipfs")
}

// Config
type Config struct {
	QriConfig     *config.Config
	EventHandlers map[event.Topic]func(payload interface{})
}

// Actor is a peer in a network simulation
type Actor struct {
	Inst  *lib.Instance
	hooks *RemoteHooks
}

// NewActor creates an actor instance
func NewActor(ctx context.Context, runenv *runtime.RunEnv, opts ...func(cfg *Config)) (*Actor, error) {
	cfg := &Config{
		QriConfig: defaultQriActorConfig(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := setup(cfg); err != nil {
		return nil, err
	}

	hooks := &RemoteHooks{runenv: runenv}

	libOpts := []lib.Option{
		lib.OptIOStreams(ioes.NewStdIOStreams()),
		lib.OptSetIPFSPath(ipfsRepoPath),
		// lib.OptSetLogAll(true),
		hooks.RemoteOptionsFunc(),
	}
	inst, err := lib.NewInstance(ctx, qriRepoPath, libOpts...)
	if err != nil {
		return nil, err
	}

	act := &Actor{
		Inst:  inst,
		hooks: hooks,
	}

	go act.subscribe(cfg.EventHandlers)
	return act, nil
}

// setup initializes on-disk IPFS & qri repos, generates private keys
func setup(cfg *Config) error {
	p := lib.SetupParams{
		SetupIPFS:   true,
		Register:    false,
		Config:      cfg.QriConfig,
		Generator:   gen.NewCryptoSource(),
		QriRepoPath: qriRepoPath,
		IPFSFsPath:  ipfsRepoPath,
	}

	return lib.Setup(p)
}

func (a *Actor) subscribe(handlers map[event.Topic]func(payload interface{})) {
	topics := make([]event.Topic, 0, len(handlers))
	for t := range handlers {
		topics = append(topics, t)
	}
	fmt.Printf("subscribing to events: %v\n%v\n", topics, handlers)
	events := a.Inst.Bus().Subscribe(topics...)
	for evt := range events {
		fmt.Printf("GOT EVENT: %s %#v\n\n", evt.Topic, evt.Payload)
		handlers[evt.Topic](evt.Payload)
	}
}

// ActorInfo carries details about an actor
type ActorInfo struct {
	Seq      int // sequence number within the test
	Peername string
	PeerID   string
	AddrInfo *peer.AddrInfo
}

// Info returns details about this actor
func (a *Actor) Info(runenv *runtime.RunEnv) *ActorInfo {
	fmt.Printf("actor repo: %v\n", a.Inst.Repo())
	pro, _ := a.Inst.Repo().Profile()
	return &ActorInfo{
		Seq:      runenv.TestInstanceCount,
		Peername: pro.Peername,
		PeerID:   pro.ID.String(),
		AddrInfo: a.AddrInfo(),
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
	fmt.Printf("actor node: %v\n", a.Inst.Node())
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
		Ref:      fmt.Sprintf("me/%s", name),
		BodyPath: csvFilepath,
	}

	ref := &reporef.DatasetRef{}
	return lib.NewDatasetRequestsInstance(a.Inst).Save(p, ref)
}

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

func defaultQriActorConfig() *config.Config {
	return &config.Config{
		Store: &config.Store{
			Type: "ipfs",
		},
		Profile: &config.ProfilePod{
			Type:    "peer",
			Color:   "",
			Created: time.Now(),
		},
		Repo: &config.Repo{
			Middleware: []string{},
			Type:       "fs",
		},
		API: &config.API{
			AllowedOrigins: []string{},
		},
		P2P: &config.P2P{
			Enabled:            true,
			QriBootstrapAddrs:  []string{},
			ProfileReplication: "full",
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
		CLI:    &config.CLI{},
		Webapp: &config.Webapp{},
		RPC:    &config.RPC{},
		Render: &config.Render{},
	}
}
