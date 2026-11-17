package peerdht

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
)

type boundedListener struct {
	net.Listener
	slots chan struct{}
}
type boundedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *boundedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }
func (l *boundedListener) Accept() (net.Conn, error) {
	l.slots <- struct{}{}
	c, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConn{Conn: c, release: func() { <-l.slots }}, nil
}
func (p *Peer) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{Handler: p, ReadHeaderTimeout: p.Config.Timeout, ReadTimeout: p.Config.Timeout, WriteTimeout: p.Config.Timeout, IdleTimeout: p.Config.Timeout, MaxHeaderBytes: 16 << 10}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			server.Close()
		case <-done:
		}
	}()
	defer p.http.CloseIdleConnections()
	err := server.Serve(&boundedListener{Listener: listener, slots: make(chan struct{}, p.Config.Limits.Workers*2)})
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
