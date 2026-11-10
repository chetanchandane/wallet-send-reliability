package main

import (
	"chaingate/internal/server"
	"chaingate/internal/wallet"
	"flag"
	"log/slog"
	"net"
	"os"
	"path/filepath"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "HTTP listen address")
	data := flag.String("data", ".local/wallet", "directory for durable demo state")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		slog.Error("demo API must bind to a loopback IP")
		os.Exit(1)
	}
	if err := run(*addr, *data); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(addr, data string) error {
	chain, err := wallet.OpenMock(filepath.Join(data, "chain.json"))
	if err != nil {
		return err
	}
	defer chain.Close()
	service, err := wallet.Open(filepath.Join(data, "wallet.json"), 50_000_000, chain)
	if err != nil {
		return err
	}
	defer service.Close()
	return server.Run(addr, wallet.Handler(service))
}
