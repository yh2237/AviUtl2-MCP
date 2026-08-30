package bridge

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

func TestClientPingAndGetContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		for range 2 {
			payload, err := protocol.ReadFrame(conn)
			if err != nil {
				serverDone <- err
				return
			}
			var req protocol.Request
			if err := json.Unmarshal(payload, &req); err != nil {
				serverDone <- err
				return
			}
			var result any
			switch req.Method {
			case "ping":
				result = protocol.PingResult{Pong: true, SessionID: "test-session", Generation: 1}
			case "get_context":
				result = protocol.Context{SessionID: "test-session", Generation: 1, Width: 1920, Height: 1080, Rate: 30, Scale: 1}
			}
			resultJSON, _ := json.Marshal(result)
			responseJSON, _ := json.Marshal(protocol.Response{ID: req.ID, Version: protocol.Version, Result: resultJSON})
			if err := protocol.WriteFrame(conn, responseJSON); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()

	client := NewClient(listener.Addr().String(), 2*time.Second)
	defer client.Close()
	ping, err := client.Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ping.Pong || ping.SessionID != "test-session" {
		t.Fatalf("unexpected ping result: %+v", ping)
	}
	contextResult, err := client.GetContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if contextResult.Width != 1920 || contextResult.Height != 1080 {
		t.Fatalf("unexpected context: %+v", contextResult)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
