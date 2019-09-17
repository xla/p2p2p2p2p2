package p2p

import (
	"errors"
)

// defaultSubChanBuffer is used for subscription channel buffer size.
const defaultSubChanBuffer = 1

var (
	ErrPeerAlreadyKnown = errors.New("peer already known")
	ErrPeerNotPresent   = errors.New("peer not present")
)

// Behaviour observed of a peer and used as the basis for promotion (e.g.
// trusted peer) or demotion (e.g. disconnect, bab with timeout).
type Behaviour string

// ChanID used to filter subscriptions of peer messages.
type ChanID = byte

// Payload is a simple alias to signify when we refer explicitly to a the bytes
// received from or send to a peer.
type Payload = []byte

// SubChan returned for subscriptions for the subscriber be infomred about new
// peer messages.
type SubChan chan SubscriptionMsg

// UnsubscribeFunc ought to be called by a subscriber who is no longer
// interested in the corresponding subscription. Shortly after the channel
// returned for the subscription will be closed.
type UnsubscribeFunc = func() error

// PeerID unique identifier for a peer.
type PeerID string

// SubscriptionMsg carries the origin in form of the PeerID and either the
// payload of sent message or a flag indicating a peer lifecycle event.
type SubscriptionMsg struct {
	peerID  PeerID
	chanID  ChanID
	payload Payload
	// TODO(xla): Rework into a proper faux enum which the consumer can switch on.
	//            Should support: peer added, peer removed.
	flag uint8
}

// Interchange routes messages between peers and subscribers.
// TODO(xla): Looks awfully lot like a Bus, maybe it's the better abstraction.
//            Something like `Broadcast` feels very natural as a potential extension of
//            this interface to support such a "gossip" concern.
type Interchange interface {
	AddPeer(PeerID, peer) error
	RemovePeer(PeerID) error

	// TODO(xla): Do we want to support a shotgun dispatch?
	// Broadcast issues a Dispatch for of the given message for all known peers.
	// Broadcast(ChanID, Payload) error

	// Dispatch a message to the peer with the matching peerID.
	//
	// Will error for cases where the peer has been disconnected as the caller
	// should not be permitted any further dispatches after a peer has been
	// removed. Yet concurrent dispatch for efficiency on the caller side could
	// lead to such cases easily.
	Dispatch(PeerID, ChanID, Payload) error

	// Subscribe to all messages matching the chanID.
	Subscribe(ChanID) (SubChan, UnsubscribeFunc, error)
}

// InterchangeLifecycle interface is expected to be satisfied by all
// long-running concrete implementations and should be used by domain compossers
// like the node.
type InterchangeLifecycle interface {
	// Run initiates routines for the Interchange.
	Run() error

	// Stop is called on a running Interchange to sginify shutdown. It is expected
	// that any implementation is cleanly releasing all resources. An error is
	// returned if any unexpected issue is encoutnered during graceful shutdown.
	Stop() error
}

// Test ProcInterchange for interface completeness.
var _ Interchange = (*ProcInterchange)(nil)
var _ InterchangeLifecycle = (*ProcInterchange)(nil)

// ProcInterchange is a concrete implementation of Interchange running entirely
// in-process.
type ProcInterchange struct {
	// Internal counter used to ensure unique subscription IDs.
	// TODO(xla): Use some collision free way of creating IDs and avoid inceasing
	//            numbers which potentially can overflow.
	id uint

	// Peers mapping to keep track of connected nodes.
	peers map[PeerID]peer
	// Active subscriptions used to demux messages from connectedpeers.
	subscriptions subscriptions

	// Channel for added peer requests.
	addPeerRequestc chan addPeerRequest
	// Channel for remove peer requests.
	removePeerRequestc chan removePeerRequest
	// Channel for dispatch requests.
	dispatchRequestc chan dispatchRequest
	// Channel for all recieved peer messages.
	receiveMsgc chan receiveMsg
	// Channel used to make it known internally that the Interchange is stopped.
	stopc chan struct{}
	// Channel for intiial stop request and to coordinate for the call side to
	// block until graceful shutdown is complete.
	stopRequestc chan chan error
	// Channel used to internally communicate a new subscription request.
	subRequestc chan subRequest
}

// NewProcInterchange returns an Interchange instance.
func NewProcInterchange() *ProcInterchange {
	return &ProcInterchange{
		id:                 1,
		peers:              map[PeerID]peer{},
		subscriptions:      subscriptions{},
		addPeerRequestc:    make(chan addPeerRequest),
		removePeerRequestc: make(chan removePeerRequest),
		dispatchRequestc:   make(chan dispatchRequest),
		receiveMsgc:        make(chan receiveMsg),
		stopc:              make(chan struct{}),
		stopRequestc:       make(chan chan error),
		subRequestc:        make(chan subRequest),
	}
}

type addPeerRequest struct {
	peerID PeerID
	peer   peer
	resc   chan error
}

type removePeerRequest struct {
	peerID PeerID
	resc   chan error
}

type peer interface {
	receive() (Payload, error)
	send(ChanID, Payload) error
}

// AddPeer implements Interchange.
func (i *ProcInterchange) AddPeer(peerID PeerID, peer peer) error {
	resc := make(chan error)
	i.addPeerRequestc <- addPeerRequest{peerID: peerID, peer: peer, resc: resc}

	return <-resc
}

// RemovePeer implements Interchange.
func (i *ProcInterchange) RemovePeer(peerID PeerID) error {
	resc := make(chan error)
	i.removePeerRequestc <- removePeerRequest{peerID: peerID, resc: resc}

	return <-resc
}

type dispatchRequest struct {
	peerID  PeerID
	chanID  ChanID
	payload Payload
	resc    chan error
}

// Dispatch implements Interchange.
func (i *ProcInterchange) Dispatch(peerID PeerID, chanID ChanID, payload Payload) error {
	resc := make(chan error)
	i.dispatchRequestc <- dispatchRequest{
		peerID:  peerID,
		chanID:  chanID,
		payload: payload,
		resc:    resc,
	}

	return <-resc
}

// Subscribe implements Interchange.
func (i *ProcInterchange) Subscribe(chanID ChanID) (SubChan, UnsubscribeFunc, error) {
	resc := make(chan subResponse)
	i.subRequestc <- subRequest{chanID: chanID, resc: resc}

	res := <-resc

	return res.subc, res.unsubscribeFunc, res.err
}

// Run implements InterchangeLifecycle.
func (i *ProcInterchange) Run() error {
	go i.dmux()

	return nil
}

// Stop implements InterchangeLifecycle.
func (i *ProcInterchange) Stop() error {
	resc := make(chan error)
	i.stopRequestc <- resc

	return <-resc
}

func (i *ProcInterchange) dmux() {
	for {
		select {
		// ADD PEER
		case req := <-i.addPeerRequestc:
			_, ok := i.peers[req.peerID]
			if ok {
				// TODO(xla): Return proper error sentinel.
				req.resc <- ErrPeerAlreadyKnown
				continue
			}

			// TODO(xla): Notify all subscriptions of added peer.
			i.peers[req.peerID] = req.peer

			req.resc <- nil

		// REMOVE PEER
		case req := <-i.removePeerRequestc:
			_, ok := i.peers[req.peerID]
			if !ok {
				// TODO(xla): Retunr proper error sentinel.
				req.resc <- ErrPeerNotPresent
				continue
			}

			// TODO(xla): Notify all subscriptions of removed peer.
			delete(i.peers, req.peerID)

			req.resc <- nil

		// DISPATCH
		case req := <-i.dispatchRequestc:
			p, ok := i.peers[req.peerID]
			if !ok {
				// TODO(xla): Return proper error sentinel.
				req.resc <- ErrPeerNotPresent
				continue
			}

			// We ensure that dispatches are satisfied as fast as possible. This way it
			// can be call in parallel and doesn't block when one of the peers is slow.
			go func(p peer, chanID ChanID, payload Payload, resc chan error) {
				resc <- p.send(chanID, payload)
			}(p, req.chanID, req.payload, req.resc)

		// RECEIVE
		case msg := <-i.receiveMsgc:
			subs, ok := i.subscriptions[msg.chanID]
			if !ok {
				continue
			}

			for _, subc := range subs {
				subc <- SubscriptionMsg{peerID: msg.peerID, chanID: msg.chanID, flag: 0}
			}

		// Stop
		case resc := <-i.stopRequestc:
			// TODO(xla): Perform graceful shutdown.
			close(i.stopc)
			resc <- nil

			return

		// Subscription
		case req := <-i.subRequestc:
			// TODO(xla): Check if stopped and return an error to the subscriber.
			//            Alternatively just expect this routine to exit when stop has been
			//            called and expect here that we can never a receive a subscriotion
			//            request after stop has been called.
			var (
				chanID = req.chanID
				nextID = i.id + 1
				subc   = make(chan SubscriptionMsg, defaultSubChanBuffer)
			)

			if _, ok := i.subscriptions[req.chanID]; !ok {
				i.subscriptions[chanID] = map[uint]SubChan{}
			}

			i.subscriptions[chanID][nextID] = subc

			// TODO(xla): Tidy up by moving this out of the case statement into a builder
			//			  or factory equivalent.
			unsubscribeFunc := func(
				subs map[uint]SubChan,
				id uint,
				subc SubChan,
			) UnsubscribeFunc {
				return func() error {
					delete(subs, id)
					close(subc)

					return nil
				}
			}(i.subscriptions[chanID], nextID, subc)

			req.resc <- subResponse{
				err:             nil,
				subc:            subc,
				unsubscribeFunc: unsubscribeFunc,
			}
		}
	}
}

func (i *ProcInterchange) isStopped() bool {
	select {
	case _, ok := <-i.stopc:
		if !ok {
			return true
		}
	default:
		// Fall through as the channel is not closed and we assume the Interchange is
		// running.
	}

	return false
}

// subscriptions is used in the interchange to keep track of the mapping of
// channel ids to active subscriptions.
type subscriptions map[ChanID]map[uint]SubChan

type subRequest struct {
	chanID ChanID
	resc   chan<- subResponse
}

type subResponse struct {
	err             error
	subc            SubChan
	unsubscribeFunc UnsubscribeFunc
}

type receiveMsg struct {
	peerID  PeerID
	chanID  ChanID
	payload Payload
}

type reporter interface {
	// report observed behaviour of peer.
	report(PeerID, Behaviour) error
}
