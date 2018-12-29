// Copyright (c) 2018 The MATRIX Authors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php

/*
the p2p/testing package provides a unit test scheme to check simple
protocol message exchanges with one pivot node and a number of dummy peers
The pivot test node runs a node.Service, the dummy peers run a mock node
that can be used to send and receive messages
*/

package testing

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"sync"
	"testing"

	"github.com/matrix/go-matrix/log"
	"github.com/matrix/go-matrix/p2p"
	"github.com/matrix/go-matrix/p2p/discover"
	"github.com/matrix/go-matrix/p2p/simulations"
	"github.com/matrix/go-matrix/p2p/simulations/adapters"
	"github.com/matrix/go-matrix/pod"
	"github.com/matrix/go-matrix/rlp"
	"github.com/matrix/go-matrix/rpc"
)

// ProtocolTester is the tester environment used for unit testing protocol
// message exchanges. It uses p2p/simulations framework
type ProtocolTester struct {
	*ProtocolSession
	network *simulations.Network
}

// NewProtocolTester constructs a new ProtocolTester
// it takes as argument the pivot node id, the number of dummy peers and the
// protocol run function called on a peer connection by the p2p server
func NewProtocolTester(t *testing.T, id discover.NodeID, n int, run func(*p2p.Peer, p2p.MsgReadWriter) error) *ProtocolTester {
	services := adapters.Services{
		"test": func(ctx *adapters.ServiceContext) (node.Service, error) {
			return &testNode{run}, nil
		},
		"mock": func(ctx *adapters.ServiceContext) (node.Service, error) {
			return newMockNode(), nil
		},
	}
	adapter := adapters.NewSimAdapter(services)
	net := simulations.NewNetwork(adapter, &simulations.NetworkConfig{})
	if _, err := net.NewNodeWithConfig(&adapters.NodeConfig{
		ID:              id,
		EnableMsgEvents: true,
		Services:        []string{"test"},
	}); err != nil {
		panic(err.Error())
	}
	if err := net.Start(id); err != nil {
		panic(err.Error())
	}

	node := net.GetNode(id).Node.(*adapters.SimNode)
	peers := make([]*adapters.NodeConfig, n)
	peerIDs := make([]discover.NodeID, n)
	for i := 0; i < n; i++ {
		peers[i] = adapters.RandomNodeConfig()
		peers[i].Services = []string{"mock"}
		peerIDs[i] = peers[i].ID
	}
	events := make(chan *p2p.PeerEvent, 1000)
	node.SubscribeEvents(events)
	ps := &ProtocolSession{
		Server:  node.Server(),
		IDs:     peerIDs,
		adapter: adapter,
		events:  events,
	}
	self := &ProtocolTester{
		ProtocolSession: ps,
		network:         net,
	}

	self.Connect(id, peers...)

	return self
}

// Stop stops the p2p server
func (t *ProtocolTester) Stop() error {
	t.Server.Stop()
	return nil
}

// Connect brings up the remote peer node and connects it using the
// p2p/simulations network connection with the in memory network adapter
func (t *ProtocolTester) Connect(selfID discover.NodeID, peers ...*adapters.NodeConfig) {
	for _, peer := range peers {
		log.Trace(fmt.Sprintf("start node %v", peer.ID))
		if _, err := t.network.NewNodeWithConfig(peer); err != nil {
			panic(fmt.Sprintf("error starting peer %v: %v", peer.ID, err))
		}
		if err := t.network.Start(peer.ID); err != nil {
			panic(fmt.Sprintf("error starting peer %v: %v", peer.ID, err))
		}
		log.Trace(fmt.Sprintf("connect to %v", peer.ID))
		if err := t.network.Connect(selfID, peer.ID); err != nil {
			panic(fmt.Sprintf("error connecting to peer %v: %v", peer.ID, err))
		}
	}

}

// testNode wraps a protocol run function and implements the node.Service
// interface
type testNode struct {
	run func(*p2p.Peer, p2p.MsgReadWriter) error
}

func (t *testNode) Protocols() []p2p.Protocol {
	return []p2p.Protocol{{
		Length: 100,
		Run:    t.run,
	}}
}

func (t *testNode) APIs() []rpc.API {
	return nil
}

func (t *testNode) Start(server *p2p.Server) error {
	return nil
}

func (t *testNode) Stop() error {
	return nil
}

// mockNode is a testNode which doesn't actually run a protocol, instead
// exposing channels so that tests can manually trigger and expect certain
// messages
type mockNode struct {
	testNode

	trigger  chan *Trigger
	expect   chan []Expect
	err      chan error
	stop     chan struct{}
	stopOnce sync.Once
}

func newMockNode() *mockNode {
	mock := &mockNode{
		trigger: make(chan *Trigger),
		expect:  make(chan []Expect),
		err:     make(chan error),
		stop:    make(chan struct{}),
	}
	mock.testNode.run = mock.Run
	return mock
}

// Run is a protocol run function which just loops waiting for tests to
// instruct it to either trigger or expect a message from the peer
func (m *mockNode) Run(peer *p2p.Peer, rw p2p.MsgReadWriter) error {
	for {
		select {
		case trig := <-m.trigger:
			m.err <- p2p.Send(rw, trig.Code, trig.Msg)
		case exps := <-m.expect:
			m.err <- expectMsgs(rw, exps)
		case <-m.stop:
			return nil
		}
	}
}

func (m *mockNode) Trigger(trig *Trigger) error {
	m.trigger <- trig
	return <-m.err
}

func (m *mockNode) Expect(exp ...Expect) error {
	m.expect <- exp
	return <-m.err
}

func (m *mockNode) Stop() error {
	m.stopOnce.Do(func() { close(m.stop) })
	return nil
}

func expectMsgs(rw p2p.MsgReadWriter, exps []Expect) error {
	matched := make([]bool, len(exps))
	for {
		msg, err := rw.ReadMsg()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		actualContent, err := ioutil.ReadAll(msg.Payload)
		if err != nil {
			return err
		}
		var found bool
		for i, exp := range exps {
			if exp.Code == msg.Code && bytes.Equal(actualContent, mustEncodeMsg(exp.Msg)) {
				if matched[i] {
					return fmt.Errorf("message #%d received two times", i)
				}
				matched[i] = true
				found = true
				break
			}
		}
		if !found {
			expected := make([]string, 0)
			for i, exp := range exps {
				if matched[i] {
					continue
				}
				expected = append(expected, fmt.Sprintf("code %d payload %x", exp.Code, mustEncodeMsg(exp.Msg)))
			}
			return fmt.Errorf("unexpected message code %d payload %x, expected %s", msg.Code, actualContent, strings.Join(expected, " or "))
		}
		done := true
		for _, m := range matched {
			if !m {
				done = false
				break
			}
		}
		if done {
			return nil
		}
	}
	for i, m := range matched {
		if !m {
			return fmt.Errorf("expected message #%d not received", i)
		}
	}
	return nil
}

// mustEncodeMsg uses rlp to encode a message.
// In case of error it panics.
func mustEncodeMsg(msg interface{}) []byte {
	contentEnc, err := rlp.EncodeToBytes(msg)
	if err != nil {
		panic("content encode error: " + err.Error())
	}
	return contentEnc
}
