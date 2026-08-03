package main

import (
	"log"
	"net/http"
	"os"
	"time"

	server "sender-server"
)

func main() {
	tzName := envOrDefault("TZ", "Asia/Shanghai")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		log.Fatalf("load TZ %q: %v", tzName, err)
	}

	dbPath := envOrDefault("DB_PATH", "data/messages.db")
	store, err := server.OpenStore(dbPath, loc)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	addr := envOrDefault("ADDR", ":8080")
	log.Printf("sender server listening on %s (TZ=%s, DB_PATH=%s)", addr, tzName, dbPath)
	if err := http.ListenAndServe(addr, server.NewHandler(store)); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
