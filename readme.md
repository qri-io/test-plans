# Qri Testground plans

We're intending to support the [testground project](https://github.com/ipfs/testground) from Protocol Labs, this repo holds our testground implementation.

🚧**Under Heavy Construction** 🚧

Testground is under active development. This is code built that sometimes depends on unmerged branches of an active project. Don't use this. Don't depend on this. 

# Test Plan Goals
We're hoping to accomplish a few things through test plans. In order, those are:

### Networked Debugging
Provide an environment for replicating bugs that are easiest to reproduce through network interaction. We have some issues that show up in [dsync](https://github.com/qri-io/dag) that are currently too difficult to debug without network simluation.

### Performance
We want to know how well the whole network performs. Quantitatively. This means measuring:

* IPFS network performance, with qri specific configurations. This is mainly a measure of which version of IPFS we're seeing most heavily deployed in the Qri network
* Qri protocol performance, independant of IPFS on a libp2p host
* Performance both networks at once

### Test Driven Decentralization
Building decentralized features for us so far has been a process of: research -> theory -> code -> local-only-tests -> production. We'd like to raise the bar for decentralized development, writing test plans that allow us to better understand how a feature will (or won't) work before it's launched. All p2p code we write should have a test plan that proves it'll perform in a small network under heavy churn, with scaling characteristics that 

### Making more p2p devs
We're planning on sending new hires here to get a better understanding of how to write p2p software, and specifically how we write p2p software. This'll be heavily dependant on two things:

* us writing a solid network simulation abstraction
* tooling to help visualize plans

# Things we don't need from testground (that others might!)
It's worth listing things we don't really need:

* Multi-language support (we're a golang shop)
* Scaling up to 1000s of nodes for tests. We'll need this one day, but that day isn't today (April 2020)
* Hand-holding from the testground team. We've wanted a project like this to exist for some time, and are willing to read source code, Issues & Pull Requests to get up to speed.