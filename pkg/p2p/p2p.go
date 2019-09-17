package p2p

import "fmt"

// Behaviour observed of a peer and used as the basis for promotion (e.g.
// trusted peer) or demotion (e.g. disconnect, bab with timeout).
type Behaviour string

// ChanID used to filter subscriptions of peer messages.
type ChanID = byte

// SubChan returned for subscriptions for the subscriber be infomred about new
// peer messages.
type SubChan chan PeerMsg

// defaultSubChanBuffer is used for subscription channel buffer size.
const defaultSubChanBuffer = 1

// UnsubscribeFunc ought to be called by a subscriber who is no longer
// interested in the corresponding subscription. Shortly after the channel
// returned for the subscription will be closed.
type UnsubscribeFunc = func() error

// PeerID unique identifier for a peer.
type PeerID string

// PeerMsg carries the origin in form of the PeerID and either the payload of
// sent message or a flag indicating a peer lifecycle event.
type PeerMsg struct {
	peerID PeerID
	msg    []byte
	flag   uint8
}

// Interchange routes messages between peers and subscribers.
type Interchange interface {
	// Dispatch a message to the peer with the matching peerID.
	//
	// Will error for cases where the peer has been disconnected.
	Dispatch(PeerID, []byte) error

	// Subscribe to all messages matching the chanID.
	// TODO(xla): Should return a close handle of thought, maybe a read-only
	//			  channel so that the caller can signal an unsubscribe.
	Subscribe(ChanID) (SubChan, UnsubscribeFunc, error)
}

// Test procInterchange for interface completeness.
var _ Interchange = (*procInterchange)(nil)

type procInterchange struct {
	// Internal counter used to ensure unique subscription IDs.
	// TODO(xla): Use some collision free way of creating IDs and avoid inceasing
	// numbers which potentially can overflow.
	id uint

	// Peers mapping to keep track of connected nodes.
	peers map[PeerID]peer
	// Active subscriptions used to demux messages from connectedpeers.
	subscriptions subscriptions

	// Channel used to internally communicate a new subscription request.
	subRequestc chan subRequest
}

// NewProcInterchange returns an Interchange instance.
func NewProcInterchange() Interchange {
	return &procInterchange{
		id:            1,
		peers:         map[PeerID]peer{},
		subscriptions: subscriptions{},
		subRequestc:   make(chan subRequest),
	}
}

func (i *procInterchange) Dispatch(_ PeerID, _ []byte) error {
	// TODO(xla): Find peer with matching id.
	// TODO(xla): Return error if peer is missing.
	// TODO(xla): Attempt send.
	// TODO(xla): Return send result.
	return fmt.Errorf("not implemented")
}

func (i *procInterchange) Subscribe(chanID ChanID) (SubChan, UnsubscribeFunc, error) {
	resc := make(chan subResponse, 1)
	i.subRequestc <- subRequest{chanID: chanID, resc: resc}

	res := <-resc

	return res.subc, res.unsubscribeFunc, res.err
}

func (i *procInterchange) dmux() error {
	for {
		select {
		case req := <-i.subRequestc:
			var (
				chanID = req.chanID
				nextID = i.id + 1
				subc   = make(chan PeerMsg, defaultSubChanBuffer)
			)

			if _, ok := i.subscriptions[req.chanID]; !ok {
				i.subscriptions[chanID] = map[uint]SubChan{}
			}

			i.subscriptions[chanID][nextID] = subc

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
