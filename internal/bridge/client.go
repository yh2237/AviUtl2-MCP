package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/yh2237/AviUtl2-MCP/internal/protocol"
)

type RemoteError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("bridge %s: %s", e.Code, e.Message)
}

type Client struct {
	address string
	timeout time.Duration

	mu     sync.Mutex
	conn   net.Conn
	nextID uint64
}

func NewClient(address string, timeout time.Duration) *Client {
	return &Client{address: address, timeout: timeout}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func (c *Client) Ping(ctx context.Context) (protocol.PingResult, error) {
	var result protocol.PingResult
	err := c.call(ctx, "ping", struct{}{}, &result)
	return result, err
}

func (c *Client) GetContext(ctx context.Context) (protocol.Context, error) {
	var result protocol.Context
	err := c.call(ctx, "get_context", struct{}{}, &result)
	return result, err
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.connectLocked(ctx); err != nil {
		return err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode bridge params: %w", err)
	}
	c.nextID++
	req := protocol.Request{ID: c.nextID, Version: protocol.Version, Method: method, Params: paramsJSON}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode bridge request: %w", err)
	}

	deadline := time.Now().Add(c.timeout)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		c.closeLocked()
		return err
	}
	if err := protocol.WriteFrame(c.conn, payload); err != nil {
		c.closeLocked()
		return fmt.Errorf("write bridge request: %w", err)
	}
	responsePayload, err := protocol.ReadFrame(c.conn)
	if err != nil {
		c.closeLocked()
		return fmt.Errorf("read bridge response: %w", err)
	}

	var response protocol.Response
	if err := json.Unmarshal(responsePayload, &response); err != nil {
		c.closeLocked()
		return fmt.Errorf("decode bridge response: %w", err)
	}
	if response.ID != req.ID {
		c.closeLocked()
		return fmt.Errorf("bridge response id mismatch: got %d, want %d", response.ID, req.ID)
	}
	if response.Version != protocol.Version {
		return fmt.Errorf("bridge protocol version mismatch: got %d, want %d", response.Version, protocol.Version)
	}
	if response.Error != nil {
		return &RemoteError{Code: response.Error.Code, Message: response.Error.Message, Retryable: response.Error.Retryable}
	}
	if len(response.Result) == 0 {
		return errors.New("bridge response has neither result nor error")
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("decode bridge result: %w", err)
	}
	return nil
}

func (c *Client) connectLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.address)
	if err != nil {
		return fmt.Errorf("connect to AviUtl2 bridge at %s: %w", c.address, err)
	}
	c.conn = conn
	return nil
}

func (c *Client) closeLocked() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}
