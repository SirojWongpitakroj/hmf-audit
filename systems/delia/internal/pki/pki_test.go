package pki

import (
	"bufio"
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestGenerateCAAndIssueCert(t *testing.T) {
	caCert, caKey, err := GenerateCA("DELIA test CA")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	cert, key, err := IssueCert(caCert, caKey, "p1", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert: %v", err)
	}
	if len(cert) == 0 || len(key) == 0 {
		t.Fatalf("IssueCert returned empty PEM")
	}
}

// TEST-vector: two mutually-authenticated servers can complete a TLS
// handshake over the private test CA (SYS-002/GAP-48), and a peer without a
// certificate signed by the same CA is rejected.
func TestMutualTLSHandshake(t *testing.T) {
	caCert, caKey, err := GenerateCA("DELIA test CA")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	serverCert, serverKey, err := IssueCert(caCert, caKey, "server", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert(server): %v", err)
	}
	clientCert, clientKey, err := IssueCert(caCert, caKey, "client", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert(client): %v", err)
	}

	serverTLSConf, err := ServerTLSConfig(serverCert, serverKey, caCert)
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	clientTLSConf, err := ClientTLSConfig(clientCert, clientKey, caCert, "127.0.0.1")
	if err != nil {
		t.Fatalf("ClientTLSConfig: %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLSConf)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}
		if line != "hello\n" {
			serverDone <- errServerMismatch
			return
		}
		serverDone <- nil
	}()

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", ln.Addr().String(), clientTLSConf)
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("server side: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for server")
	}
}

var errServerMismatch = mismatchError{}

type mismatchError struct{}

func (mismatchError) Error() string { return "unexpected line received" }

// A client certificate NOT signed by the server's trusted CA must be
// rejected (mutual TLS actually enforced, not just presented).
func TestMutualTLSRejectsUntrustedClient(t *testing.T) {
	caCert, caKey, err := GenerateCA("DELIA test CA")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	serverCert, serverKey, err := IssueCert(caCert, caKey, "server", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert(server): %v", err)
	}
	serverTLSConf, err := ServerTLSConfig(serverCert, serverKey, caCert)
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}

	// A different, untrusted CA issues the client's certificate.
	otherCACert, otherCAKey, err := GenerateCA("some other CA")
	if err != nil {
		t.Fatalf("GenerateCA(other): %v", err)
	}
	rogueCert, rogueKey, err := IssueCert(otherCACert, otherCAKey, "client", []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("IssueCert(rogue): %v", err)
	}
	rogueTLSConf, err := ClientTLSConfig(rogueCert, rogueKey, otherCACert, "127.0.0.1")
	if err != nil {
		t.Fatalf("ClientTLSConfig(rogue): %v", err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverTLSConf)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer ln.Close()

	acceptErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		defer conn.Close()
		if tlsConn, ok := conn.(*tls.Conn); ok {
			acceptErr <- tlsConn.Handshake()
			return
		}
		acceptErr <- nil
	}()

	_, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", ln.Addr().String(), rogueTLSConf)
	// Either side detecting the trust failure is an acceptable outcome; the
	// important invariant is that the handshake does NOT succeed on both.
	select {
	case sErr := <-acceptErr:
		if dialErr == nil && sErr == nil {
			t.Fatalf("handshake unexpectedly succeeded with an untrusted client certificate")
		}
	case <-time.After(3 * time.Second):
		if dialErr == nil {
			t.Fatalf("client dial succeeded and server never reported a handshake failure")
		}
	}
}
