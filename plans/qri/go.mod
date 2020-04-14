module github.com/ipfs/testground/plans/qri

go 1.13

replace github.com/ipfs/testground/sdk/runtime => ../../sdk/runtime

require (
	github.com/ipfs/testground/sdk/runtime v0.4.0
	github.com/ipfs/testground/sdk/sync v0.4.0
	github.com/libp2p/go-libp2p-core v0.2.3
	github.com/qri-io/dataset v0.1.5-0.20200410214416-1a724f5032df
	github.com/qri-io/ioes v0.1.1
	github.com/qri-io/qri v0.9.8-0.20200414131921-2b904f5e35d8
)
