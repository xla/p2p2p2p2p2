package p2p

import (
	"fmt"
	"reflect"
	"testing"
)

func TestInterchangeLifecycle(t *testing.T) {
	ic := NewProcInterchange()

	if err := ic.Run(); err != nil {
		t.Fatal(err)
	}

	if err := ic.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestInterchangeDispatch(t *testing.T) {
	var (
		ic = NewProcInterchange()
		tp = &testPeer{}

		peerID  = PeerID("test-peer")
		chanID  = byte(0x1)
		payload = []byte("test payload")
	)
	defer ic.Stop()

	if err := ic.Run(); err != nil {
		t.Fatal(err)
	}

	if have, want := ic.RemovePeer(peerID), ErrPeerNotPresent; have != want {
		t.Errorf("have %v, want %v", have, want)
	}

	if err := ic.AddPeer(peerID, tp); err != nil {
		t.Fatal(err)
	}

	if have, want := ic.AddPeer(peerID, tp), ErrPeerAlreadyKnown; have != want {
		t.Errorf("have %v, want %v", have, want)
	}

	if err := ic.Dispatch(peerID, chanID, payload); err != nil {
		t.Fatal(err)
	}

	sendID, sendPayload := tp.last()

	if have, want := sendID, chanID; have != want {
		t.Errorf("have %v, want %v", have, want)
	}

	if have, want := sendPayload, payload; !reflect.DeepEqual(have, want) {
		t.Errorf("have %v, want %v", have, want)
	}

	if err := ic.RemovePeer(peerID); err != nil {
		t.Fatal(err)
	}

	if have, want := ic.Dispatch(peerID, chanID, payload), ErrPeerNotPresent; have != want {
		t.Fatalf("have %v, want %v", have, want)
	}
}

type testPeer struct {
	lastChanID  ChanID
	lastPayload Payload
}

func (p *testPeer) receive() (Payload, error) { return nil, fmt.Errorf("not implemented") }
func (p *testPeer) send(chanID ChanID, payload Payload) error {
	p.lastChanID, p.lastPayload = chanID, payload

	return nil
}
func (p *testPeer) last() (ChanID, Payload) { return p.lastChanID, p.lastPayload }
