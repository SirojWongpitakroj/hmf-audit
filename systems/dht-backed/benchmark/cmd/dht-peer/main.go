package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"

	contract "canonicalingestion"
	"dhtbacked/internal/peerdht"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	envFile := flag.String("env-file", "", "optional node-local deployment environment profile")
	path := flag.String("config", "", "this peer's deployment JSON")
	flag.Parse()
	if err := contract.LoadEnv(*envFile); err != nil {
		return err
	}
	d, c, err := peerdht.LoadDeployment(*path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	db, err := contract.OpenPostgres(ctx, os.Getenv("DHT_PEER_POSTGRES_DSN"), c.Limits.Workers+2)
	if err != nil {
		return err
	}
	defer db.Close()
	local := &peerdht.LocalStore{DB: db, Node: c.Node, Overlay: c.Overlay, MaxValueBytes: c.Limits.MaxValueBytes}
	peer, err := peerdht.NewPeer(ctx, c, local)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", d.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Printf("DHT peer=%d overlay=%s listen=%s persistent_store=PostgreSQL\n", c.Node, c.Overlay, listener.Addr())
	return peer.Serve(ctx, listener)
}
