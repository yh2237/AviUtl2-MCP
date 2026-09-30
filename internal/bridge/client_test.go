package bridge

import (
	"context"
	"encoding/json"
	"errors"
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
	log := client.RecentCalls()
	if len(log) != 2 || log[0].Method != "ping" || log[1].Method != "get_context" || log[0].Error != "" {
		t.Fatalf("unexpected call log: %+v", log)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestMutationSendsOptimisticContext(t *testing.T) {
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
		if req.Method != "add_text" || req.Context == nil || req.Context.SessionID != "session-1" ||
			req.Context.Generation == nil || *req.Context.Generation != 7 ||
			req.Context.SceneID == nil || *req.Context.SceneID != 3 {
			serverDone <- errors.New("mutation request did not contain expected context")
			return
		}
		result := protocol.MutationResult{
			Context: protocol.Context{SessionID: "session-1", Generation: 7, SceneID: 3},
			Results: []protocol.OperationResult{{Index: 0, Op: "add_text", Changed: true}},
		}
		resultJSON, _ := json.Marshal(result)
		responseJSON, _ := json.Marshal(protocol.Response{ID: req.ID, Version: protocol.Version, Result: resultJSON})
		serverDone <- protocol.WriteFrame(conn, responseJSON)
	}()

	generation, sceneID := uint64(7), 3
	client := NewClient(listener.Addr().String(), 2*time.Second)
	defer client.Close()
	result, err := client.AddText(context.Background(), protocol.AddTextParams{
		Text: "hello", Layer: 0, Frame: 0, Length: 30, Size: 34, Color: "ffffff",
	}, &protocol.ExpectedContext{SessionID: "session-1", Generation: &generation, SceneID: &sceneID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || !result.Results[0].Changed {
		t.Fatalf("unexpected mutation result: %+v", result)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestRemoteErrorPreservesDetails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		payload, _ := protocol.ReadFrame(conn)
		var req protocol.Request
		_ = json.Unmarshal(payload, &req)
		details := json.RawMessage(`{"expected":4,"actual":5}`)
		responseJSON, _ := json.Marshal(protocol.Response{
			ID: req.ID, Version: protocol.Version,
			Error: &protocol.Error{Code: "STALE_CONTEXT", Message: "changed", Details: details},
		})
		_ = protocol.WriteFrame(conn, responseJSON)
	}()

	client := NewClient(listener.Addr().String(), 2*time.Second)
	defer client.Close()
	_, err = client.GetContext(context.Background())
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code != "STALE_CONTEXT" || len(remote.Details) == 0 {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestClientCancellationInterruptsReadAndReconnects(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requestReceived := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		if _, err := protocol.ReadFrame(conn); err != nil {
			serverDone <- err
			return
		}
		close(requestReceived)
		if _, err := protocol.ReadFrame(conn); err == nil {
			serverDone <- errors.New("cancelled connection was reused")
			return
		}
		next, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer next.Close()
		payload, err := protocol.ReadFrame(next)
		if err != nil {
			serverDone <- err
			return
		}
		var req protocol.Request
		if err := json.Unmarshal(payload, &req); err != nil {
			serverDone <- err
			return
		}
		response, _ := json.Marshal(protocol.Response{
			ID: req.ID, Version: protocol.Version, Result: json.RawMessage(`{"pong":true}`),
		})
		serverDone <- protocol.WriteFrame(next, response)
	}()
	client := NewClient(listener.Addr().String(), 10*time.Second)
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := client.Ping(ctx)
		callDone <- err
	}()
	select {
	case <-requestReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach bridge")
	}
	cancel()
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not interrupt bridge read")
	}
	if result, err := client.Ping(context.Background()); err != nil || !result.Pong {
		t.Fatalf("reconnect failed: %+v, %v", result, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsCancelledContextOnExistingConnection(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	client := NewClient("unused", time.Second)
	client.conn = conn
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if client.nextID != 0 {
		t.Fatal("cancelled request should not be sent")
	}
}

func TestCallTimeoutOverrideAllowsSlowSDKResponse(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	client := NewClient("unused", time.Millisecond)
	client.conn = conn
	defer client.Close()
	serverDone := make(chan error, 1)
	go func() {
		payload, err := protocol.ReadFrame(peer)
		if err != nil {
			serverDone <- err
			return
		}
		var req protocol.Request
		if err := json.Unmarshal(payload, &req); err != nil {
			serverDone <- err
			return
		}
		time.Sleep(50 * time.Millisecond)
		response, _ := json.Marshal(protocol.Response{ID: req.ID, Version: protocol.Version, Result: json.RawMessage(`{"pong":true}`)})
		serverDone <- protocol.WriteFrame(peer, response)
	}()
	ctx := WithCallTimeout(context.Background(), 3*time.Second)
	if result, err := client.Ping(ctx); err != nil || !result.Pong {
		t.Fatalf("timeout override failed: %+v, %v", result, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
