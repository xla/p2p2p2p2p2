package main

import (
	"github.com/tendermint/p2p2p2p2p2/pkg/p2p"
)

type reactor struct {
	interchange p2p.Interchange
}

func newReactor(i p2p.Interchange) reactor {
	return reactor{interchange: i}
}

func main() {
	ic := p2p.NewProcInterchange()

	go func(ic p2p.InterchangeLifecycle) {
		panic(ic.Run())
	}(ic)

	_ = newReactor(ic)
}
