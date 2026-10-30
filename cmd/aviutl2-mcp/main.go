package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yh2237/AviUtl2-MCP/internal/bridge"
	"github.com/yh2237/AviUtl2-MCP/internal/mcpserver"
	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

// version is stamped by scripts/package.ps1 (-X main.version=...). A plain
// `go build` keeps "dev" so the reported server version can never be mistaken
// for a released build.
var version = "dev"

func main() {
	address := envOrDefault("AVIUTL2_MCP_BRIDGE_ADDR", protocol.DefaultAddress)
	timeout := durationFromEnv("AVIUTL2_MCP_BRIDGE_TIMEOUT", 5*time.Second)
	client := bridge.NewClient(address, timeout)
	defer client.Close()

	server := mcpserver.New(client, version)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("invalid %s: %v", name, err)
	}
	return duration
}
