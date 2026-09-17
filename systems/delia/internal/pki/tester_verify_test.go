package pki

// Independent tester-agent verification: the developer's own
// TestMutualTLSRejectsUntrustedClient only checks a client cert signed by
// the WRONG CA. This adds the other half of "mutual TLS is actually
// enforced": a client that presents NO certificate at all must also be
// rejected by a real handshake (RequireAndVerifyClientCert must actually be
// enforced, not just configured and silently ignored by the stdlib config
// wiring).

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"
)

func TestTesterMutualTLSRejectsClientWithNoCertificate(t *testing.T) {
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

	// A client config that trusts the real CA for the server side, but
	// presents no client certificate at all.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		t.Fatalf("failed to parse CA certificate into pool")
	}
	bareClientConf := &tls.Config{
		RootCAs:    pool,
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS12,
		// Certificates deliberately left empty.
	}

	_, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", ln.Addr().String(), bareClientConf)

	select {
	case sErr := <-acceptErr:
		if dialErr == nil && sErr == nil {
			t.Fatalf("handshake unexpectedly succeeded with no client certificate presented at all")
		}
	case <-time.After(3 * time.Second):
		if dialErr == nil {
			t.Fatalf("client dial succeeded and server never reported a handshake failure for a certificate-less client")
		}
	}
}
