package p2p

import "fmt"

// defaultSubChanBuffer is used for subscription channel buffer size.
const defaultSubChanBuffer = 1

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
	//			  Should support: peer added, peer removed.
	flag uint8
}

// Interchange routes messages between peers and subscribers.
// TODO(xla): Looks awfully lot like a Bus, maybe it's the better abstraction.
//			  Something like `Broadcast` feels very natural as a potential extension of
//			  this interface to support such a "gossip" concern.
type Interchange interface {
	// Add(PeerID, peer) error
	// Remove(PeerID) error

	// TODO(xla): Do we want to support a shotgun dispatch?
	// Broadcast issues a Dispatch for of the given message for all known peers.
	// Broadcast(ChanID, Payload) error

	// Dispatch a message to the peer with the matching peerID.
	//
	// Will error for cases where the peer has been disconnected as the caller
	// should not be permitted any further dispatches after a peer has been
	// removed. Yet concurrent disaptch for efficiency on the caller side could
	// lead to such cases easily.
	Dispatch(PeerID, ChanID, Payload) error

	// Subscribe to all messages matching the chanID.
	Subscribe(ChanID) (SubChan, UnsubscribeFunc, error)
}

// InterchangeLifecycle interface is expected to be satisfied by all
// long-running concrete implementations and should be used by domain compossers
// like the node.
type InterchangeLifecycle interface {
	// Run is blocking and will return only return when either Stop is called or
	// the long-running engine can't recover.
	// TODO(xla): Should Run really block? It makes it awkared in testing, which is
	//			  either a good sign with regards to synchronous APIs or a bad sign as it
	//			  indicates bad ergonomics for the caller.
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
	//			  numbers which potentially can overflow.
	id uint

	// Peers mapping to keep track of connected nodes.
	peers map[PeerID]peer
	// Active subscriptions used to demux messages from connectedpeers.
	subscriptions subscriptions

	// Channel used to make it known internally that the Interchange is stopped.
	stopc chan struct{}
	// Channel for intiial stop request and to coordinate for the call side to
	// block until graceful shutdown is complete.
	stopRequestc chan stopRequest
	// Channel used to internally communicate a new subscription request.
	subRequestc chan subRequest
}

// NewProcInterchange returns an Interchange instance.
func NewProcInterchange() *ProcInterchange {
	return &ProcInterchange{
		id:            1,
		peers:         map[PeerID]peer{},
		subscriptions: subscriptions{},
		stopc:         make(chan struct{}),
		stopRequestc:  make(chan stopRequest),
		subRequestc:   make(chan subRequest),
	}
}

// Dispatch implements Interchange.
func (i *ProcInterchange) Dispatch(_ PeerID, _ ChanID, _ Payload) error {
	// TODO(xla): Find peer with matching id.
	// TODO(xla): Return error if peer is missing.
	// TODO(xla): Attempt send.
	// TODO(xla): Return send result.
	return fmt.Errorf("not implemented")
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
	for {
		select {
		// Stop
		case req := <-i.stopRequestc:
			// TODO(xla): Initiate graceful shutdown.
			close(i.stopc)

			err := fmt.Errorf("stop not implemented")
			req.resc <- err
			return err

		// Subscription
		case req := <-i.subRequestc:
			// TODO(xla): Check if stopped and return an error to the subscriber.
			//			  Alternatively just expect this routine to exit when stop has been
			//			  called and expect here that we can never a receive a subscriotion
			//			  request after stop has been called.
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

// Stop implements InterchangeLifecycle.
func (i *ProcInterchange) Stop() error {
	resc := make(chan error)
	i.stopRequestc <- stopRequest{resc: resc}

	return <-resc
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

type stopRequest struct {
	resc chan error
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

type peer interface {
	receive() ([]byte, error)
	send([]byte) error
}

type reporter interface {
	// report observed behaviour of peer.
	report(PeerID, Behaviour) error
}
