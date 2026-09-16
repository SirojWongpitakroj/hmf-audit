// Package nettransport is the real TLS/TCP transport for off-chain DELIA
// messages: the concrete "own TLS endpoint" half of the GAP-54 process-model
// ruling ("one OS process per DELIA server, each with its own TLS endpoint
// and a sequential protocol handler"). It carries frames in the GAP-47/73
// format (internal/spec.WriteFrame/ReadFrame) over a mutual-TLS connection
// (internal/pki).
//
// Concurrency here is I/O concurrency only — one goroutine per accepted
// connection reads frames and forwards them to a single shared channel; one
// goroutine drains that channel and invokes the handler sequentially. No
// goroutine parallelizes a protocol step (GO-003/GO-004): this is exactly
// the "concurrency needed only to model independent servers receiving
// messages asynchronously" that GAP-54 says is not an optimization.
package nettransport

import (
	"crypto/tls"
	"errors"
	"net"
	"sync"

	"delia/internal/spec"
)

// Inbound is one received frame, with the address it arrived from.
type Inbound struct {
	From    net.Addr
	Type    spec.MessageType
	Payload []byte
}

// Handler processes one inbound frame at a time (the sequential protocol
// handler).
type Handler func(Inbound)

// Server listens for incoming mTLS connections and dispatches every frame,
// from every connection, to a single Handler sequentially.
type Server struct {
	ln      net.Listener
	handler Handler

	inbox chan Inbound

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// Listen starts listening on addr with the given mTLS config. Call Serve to
// begin dispatching.
func Listen(addr string, tlsConfig *tls.Config) (*Server, error) {
	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return nil, err
	}
	return &Server{ln: ln, inbox: make(chan Inbound, 64)}, nil
}

// Addr returns the listener's bound address.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Serve accepts connections and processes their frames one at a time
// through handler, on a single goroutine (the sequential protocol handler).
// It blocks until the listener is closed.
func (s *Server) Serve(handler Handler) error {
	s.handler = handler
	go s.dispatchLoop()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				close(s.inbox)
				s.wg.Wait()
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go s.readLoop(conn)
	}
}

func (s *Server) readLoop(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	for {
		typ, payload, err := spec.ReadFrame(conn)
		if err != nil {
			return
		}
		s.inbox <- Inbound{From: conn.RemoteAddr(), Type: typ, Payload: payload}
	}
}

func (s *Server) dispatchLoop() {
	for msg := range s.inbox {
		if s.handler != nil {
			s.handler(msg)
		}
	}
}

// Close stops accepting new connections.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.ln.Close()
}

// Conn is an outbound mTLS connection used to send frames to one peer.
type Conn struct {
	c  net.Conn
	mu sync.Mutex
}

// Dial opens a mutual-TLS connection to addr.
func Dial(addr string, tlsConfig *tls.Config) (*Conn, error) {
	c, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return nil, err
	}
	return &Conn{c: c}, nil
}

// Send writes one frame (GAP-47/73 layout). Safe for concurrent use by
// multiple callers (serializes writes on the wire; does not parallelize any
// protocol step).
func (c *Conn) Send(msgType spec.MessageType, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.c == nil {
		return errors.New("nettransport: connection is closed")
	}
	return spec.WriteFrame(c.c, msgType, payload)
}

// Close closes the connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.c == nil {
		return nil
	}
	err := c.c.Close()
	c.c = nil
	return err
}
