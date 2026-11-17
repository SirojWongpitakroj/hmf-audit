package peerdht

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	contract "canonicalingestion"
	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
)

func TestPeerProcessHelper(t *testing.T) {
	if os.Getenv("DHT_PROCESS_HELPER") != "1" {
		return
	}
	var c Config
	if err := json.Unmarshal([]byte(os.Getenv("DHT_PROCESS_CONFIG")), &c); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := contract.OpenPostgres(ctx, os.Getenv("DHT_PROCESS_DSN"), c.Limits.Workers+2)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := NewPeer(ctx, c, &LocalStore{DB: db, Node: c.Node, Overlay: c.Overlay, MaxValueBytes: c.Limits.MaxValueBytes})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", os.Getenv("DHT_PROCESS_ADDR"))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("peer-ready")
	if err = p.Serve(ctx, listener); err != nil {
		t.Fatal(err)
	}
}
func TestFourPersistentPeerProcesses(t *testing.T) {
	dsn := os.Getenv("DHT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("DHT_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	var databases, dsns, addresses, peers []string
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("peer_test_%d_%d", time.Now().UnixNano(), i)
		if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
			t.Fatal(err)
		}
		databases = append(databases, name)
		copyURL := *parsed
		copyURL.Path = "/" + name
		dsns = append(dsns, copyURL.String())
		listener, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		addresses = append(addresses, listener.Addr().String())
		peers = append(peers, "http://"+listener.Addr().String())
		listener.Close()
	}
	defer func() {
		for _, name := range databases {
			admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		}
	}()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := auth.NewCA(priv, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	limits := Limits{Workers: 2, MaxRequests: 1000, QueueBytes: 512, MaxValueBytes: 4096, MaxRequestBytes: 16384, ProofBytes: 1 << 20, AncestorMapBytes: 2 << 20, CacheBytes: 1024, CacheEntries: 3, RecordFetchBatch: 4, RecordBytes: 1 << 20}
	cfg := Config{Node: 0, Overlay: "test-four-peers", Peers: peers, CA: pub, WriteSecret: "isolated-test-only", Limits: limits, Timeout: 10 * time.Second}
	commands := make([]*exec.Cmd, 4)
	stop := func(i int) {
		if commands[i] != nil {
			commands[i].Process.Kill()
			commands[i].Wait()
			commands[i] = nil
		}
	}
	defer func() {
		for i := range commands {
			stop(i)
		}
	}()
	start := func(i int) {
		t.Helper()
		executable, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		node := cfg
		node.Node = i
		body, _ := json.Marshal(node)
		cmd := exec.Command(executable, "-test.run=^TestPeerProcessHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(), "DHT_PROCESS_HELPER=1", "DHT_PROCESS_CONFIG="+string(body), "DHT_PROCESS_DSN="+dsns[i], "DHT_PROCESS_ADDR="+addresses[i])
		output, e := cmd.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stderr = os.Stderr
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		commands[i] = cmd
		ready := make(chan string, 1)
		go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case line := <-ready:
			if line != "peer-ready\n" {
				t.Fatalf("peer %d did not start: %q", i, line)
			}
		case <-timer.C:
			t.Fatalf("peer %d startup timed out", i)
		}
	}
	for i := 0; i < 4; i++ {
		start(i)
	}
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	token := ca.Issue("test-auditor", 1, []string{"application"})
	var requests []dht.ParentRequest
	represented := map[int]bool{}
	for i := uint64(0); i < 40; i++ {
		tree := dht.TreeID(1, "application")
		key := dht.KeyP(tree, 8, i)
		record := dht.ParentNode{TreeID: tree, J: 8, K: i, Node: hashing.H([]byte(fmt.Sprint(i))), CommitSeq: 40}
		if err = client.Put(key, record.Serialize()); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, dht.ParentRequest{Epoch: 1, LogType: "application", J: 8, K: i, Key: key})
		represented[Owner(key)] = true
	}
	if len(represented) != 4 {
		t.Fatal("test does not cover all peer owners")
	}
	for _, req := range requests {
		b, e := client.GetParent(&token, req)
		if e != nil {
			t.Fatal(e)
		}
		p, e := dht.ParseParentNode(b)
		if e != nil || p.K != req.K {
			t.Fatal("native parent record changed", e)
		}
	}
	if s := client.Stats(); s.Hops == 0 || s.StorageReads == 0 || s.AncestorRecords != 40 {
		t.Fatal(s)
	}
	// Kill every peer process: no in-process key/value or cache survives.
	for i := 0; i < 4; i++ {
		stop(i)
	}
	client.Close()
	for i := 0; i < 4; i++ {
		start(i)
	}
	client, err = NewClient(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, req := range requests {
		if _, e := client.GetParent(&token, req); e != nil {
			t.Fatal("authentication state lost on restart", e)
		}
	}
	for i := 0; i < 4; i++ {
		resp, e := http.Get(peers[i] + "/stats")
		if e != nil {
			t.Fatal(e)
		}
		var stats Stats
		e = json.NewDecoder(resp.Body).Decode(&stats)
		resp.Body.Close()
		if e != nil || stats.CacheBytes > 1024 || stats.CacheEntries > 3 || stats.StorageReads == 0 {
			t.Fatal("cache bound/restart reads", i, stats, e)
		}
	}
	req := requests[0]
	original, e := client.GetParent(&token, req)
	if e != nil {
		t.Fatal(e)
	}
	if e = client.Put(req.Key, original); e != nil {
		t.Fatal("identical publication replay", e)
	}
	if e = client.Put(req.Key, []byte("different")); e == nil {
		t.Fatal("conflicting replay accepted")
	}
	expired := token
	expired.Body.ExpiresAtUnix = 1
	if _, e = client.GetParent(&expired, req); e == nil {
		t.Fatal("invalid authorization accepted from warm cache")
	}
	// A second logical node cannot point at another node's persistent database.
	wrongDB, e := sql.Open("pgx", dsns[0])
	if e != nil {
		t.Fatal(e)
	}
	defer wrongDB.Close()
	wrong := cfg
	wrong.Node = 1
	if _, e = NewPeer(ctx, wrong, &LocalStore{DB: wrongDB, Node: 1, Overlay: wrong.Overlay, MaxValueBytes: 4096}); e == nil {
		t.Fatal("centralized shared peer store accepted")
	}
}
