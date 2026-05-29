package main

import (
	"log"
	"net/http"
	"os"

	"github.com/RasmusHS/p2pft/internal/relay"
)

func main() {
	addr := os.Getenv("RELAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := relay.NewServer()

	mux := http.NewServeMux()
	mux.Handle("/ws", server)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("relay listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
