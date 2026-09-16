package nettransport

import (
	"testing"
	"time"

	"delia/internal/pki"
	"delia/internal/spec"
)

func mustCerts(t *testing.T) (serverCert, serverKey, clientCert, clientKey, caCert []byte) {
	t.Helper()
	ca, caKey, err := pki.GenerateCA("DELIA test CA")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	sc, sk, err := pki.IssueCert(ca, caKey, "server", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert(server): %v", err)
	}
	cc, ck, err := pki.IssueCert(ca, caKey, "client", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert(client): %v", err)
	}
	return sc, sk, cc, ck, ca
}

// End-to-end: a real mTLS + framed message round trip (NET-004-shaped
// payload, but content-agnostic here — this package only tests the
// transport, not message semantics).
func TestServeAndSendRoundTrip(t *testing.T) {
	serverCert, serverKey, clientCert, clientKey, caCert := mustCerts(t)

	serverTLS, err := pki.ServerTLSConfig(serverCert, serverKey, caCert)
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	clientTLS, err := pki.ClientTLSConfig(clientCert, clientKey, caCert, "127.0.0.1")
	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}

	srv, err := Listen("127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	received := make(chan Inbound, 4)
	go srv.Serve(func(in Inbound) {
		received <- in
	})

	conn, err := Dial(srv.Addr().String(), clientTLS)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if err := conn.Send(spec.MsgRootState, []byte{0xAA, 0xBB}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := conn.Send(spec.MsgFailureNotification, []byte{0xCC}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var got []Inbound
	for i := 0; i < 2; i++ {
		select {
		case m := <-received:
			got = append(got, m)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for message %d", i)
		}
	}

	if got[0].Type != spec.MsgRootState || string(got[0].Payload) != "\xAA\xBB" {
		t.Fatalf("first message mismatch: %+v", got[0])
	}
	if got[1].Type != spec.MsgFailureNotification || string(got[1].Payload) != "\xCC" {
		t.Fatalf("second message mismatch: %+v", got[1])
	}
}

// Multiple concurrent connections are all delivered through the single
// sequential handler (no protocol-step parallelism, GAP-54): this test only
// checks that both connections' messages arrive; ordering across distinct
// connections is not defined by GAP-54 and is not asserted here, only that
// each connection's own frames are not corrupted.
func TestServeMultipleConnections(t *testing.T) {
	serverCert, serverKey, clientCert, clientKey, caCert := mustCerts(t)
	serverTLS, _ := pki.ServerTLSConfig(serverCert, serverKey, caCert)
	clientTLS, _ := pki.ClientTLSConfig(clientCert, clientKey, caCert, "127.0.0.1")

	srv, err := Listen("127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	received := make(chan Inbound, 8)
	go srv.Serve(func(in Inbound) { received <- in })

	var conns []*Conn
	for i := 0; i < 3; i++ {
		c, err := Dial(srv.Addr().String(), clientTLS)
		if err != nil {
			t.Fatalf("Dial %d: %v", i, err)
		}
		defer c.Close()
		if err := c.Send(spec.MsgRootState, []byte{byte(i)}); err != nil {
			t.Fatalf("Send %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	seen := map[byte]bool{}
	for i := 0; i < 3; i++ {
		select {
		case m := <-received:
			if len(m.Payload) != 1 {
				t.Fatalf("unexpected payload %x", m.Payload)
			}
			seen[m.Payload[0]] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for connection %d's message", i)
		}
	}
	for i := byte(0); i < 3; i++ {
		if !seen[i] {
			t.Fatalf("did not receive message from connection %d", i)
		}
	}
}
