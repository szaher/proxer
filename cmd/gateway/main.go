package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/szaher/try/proxer/internal/gateway"
)

func main() {
	cfg, err := gateway.LoadConfigFromEnv()
	if err != nil {
		log.Fatalf("load gateway config: %v", err)
	}

	logger := log.New(os.Stdout, "[gateway] ", log.LstdFlags|log.Lmicroseconds)
	server := gateway.NewServer(cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Printf("starting proxer gateway on %s", cfg.ListenAddr)
	if err := server.Start(ctx); err != nil {
		logger.Fatalf("gateway stopped with error: %v", err)
	}
	logger.Printf("gateway shutdown complete")
}
