package p2p

import (
	"testing"
)

func TestInterchangeStop(t *testing.T) {
	var (
		ic    = NewProcInterchange()
		stopc = make(chan struct{})
	)

	go func(il InterchangeLifecycle, stopc chan struct{}) {
		defer close(stopc)

		if err := il.Stop(); err != nil {
			t.Fatal(err)
		}
	}(ic, stopc)

	if err := ic.Run(); err != nil {
		t.Fatal(err)
	}

	<-stopc
}
