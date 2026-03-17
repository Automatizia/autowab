package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/automatizia/autowab/internal/api"
	"github.com/automatizia/autowab/internal/whatsapp"
	"go.uber.org/zap"
)

var (
	port    = flag.Int("port", 3005, "HTTP server port")
	dbPath  = flag.String("db", "./autowab.db", "SQLite database path")
	webhook = flag.String("webhook", "", "Webhook URL for incoming messages")
	token   = flag.String("token", "", "API bearer token (required)")
)

func main() {
	flag.Parse()

	log, _ := zap.NewProduction()
	defer log.Sync()

	if *token == "" {
		if t := os.Getenv("AUTOWAB_TOKEN"); t != "" {
			*token = t
		} else {
			fmt.Fprintln(os.Stderr, "Error: --token or AUTOWAB_TOKEN required")
			os.Exit(1)
		}
	}

	// Boot WhatsApp client
	wac, err := whatsapp.New(*dbPath, *webhook, log)
	if err != nil {
		log.Fatal("whatsapp init failed", zap.Error(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := wac.Connect(ctx); err != nil {
		log.Fatal("whatsapp connect failed", zap.Error(err))
	}

	// Boot HTTP API
	srv := api.New(wac, *token, log)

	go func() {
		addr := fmt.Sprintf(":%d", *port)
		log.Info("autowab HTTP server starting", zap.String("addr", addr))
		if err := srv.Run(addr); err != nil {
			log.Fatal("server error", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutting down autowab...")
	wac.Disconnect()
}
