package main

import (
	"log"
	"net/http"
	"os"

	"inner-companion/internal/agent"
	"inner-companion/internal/gateway"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	runner := agent.EchoRunner{}
	svc := gateway.NewService(runner)
	h := gateway.NewHTTPHandler(svc)

	addr := ":" + port
	log.Printf("gateway listening on %s", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		log.Fatal(err)
	}
}
