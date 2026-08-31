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
	Details   json.RawMessage
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("bridge %s: %s", e.Code, e.Message)
}

type Client struct {
	address string
	timeout time.Duration

	mu      sync.Mutex
	conn    net.Conn
	nextID  uint64
	history []CallLog
}

type CallLog struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

func NewClient(address string, timeout time.Duration) *Client {
	return &Client{address: address, timeout: timeout}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func (c *Client) Reconnect(ctx context.Context) (protocol.PingResult, error) {
	c.mu.Lock()
	_ = c.closeLocked()
	c.mu.Unlock()
	return c.Ping(ctx)
}

func (c *Client) RecentCalls() []CallLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]CallLog(nil), c.history...)
}

func (c *Client) Ping(ctx context.Context) (protocol.PingResult, error) {
	return call[protocol.PingResult](c, ctx, "ping", struct{}{}, nil)
}

func (c *Client) GetContext(ctx context.Context) (protocol.Context, error) {
	return call[protocol.Context](c, ctx, "get_context", struct{}{}, nil)
}

func (c *Client) InspectTimeline(ctx context.Context, params protocol.InspectTimelineParams) (protocol.TimelineResult, error) {
	return call[protocol.TimelineResult](c, ctx, "inspect_timeline", params, nil)
}

func (c *Client) InspectObject(ctx context.Context, params protocol.InspectObjectParams) (protocol.ObjectResult, error) {
	return call[protocol.ObjectResult](c, ctx, "inspect_object", params, nil)
}

func (c *Client) InspectObjects(ctx context.Context, params protocol.InspectObjectsParams) (protocol.ObjectsResult, error) {
	return call[protocol.ObjectsResult](c, ctx, "inspect_objects", params, nil)
}

func (c *Client) InspectObjectValues(ctx context.Context, params protocol.InspectObjectValuesParams) (protocol.ObjectValuesResult, error) {
	return call[protocol.ObjectValuesResult](c, ctx, "inspect_object_values", params, nil)
}

func (c *Client) ListEffects(ctx context.Context) ([]protocol.EffectDefinition, error) {
	return call[[]protocol.EffectDefinition](c, ctx, "list_effects", struct{}{}, nil)
}

func (c *Client) ListEffectItems(ctx context.Context, effect string) (protocol.EffectItemsResult, error) {
	return call[protocol.EffectItemsResult](c, ctx, "list_effect_items", protocol.ListEffectItemsParams{Effect: effect}, nil)
}

func (c *Client) GetSelection(ctx context.Context) (protocol.SelectionResult, error) {
	return call[protocol.SelectionResult](c, ctx, "get_selection", struct{}{}, nil)
}

func (c *Client) PreflightMedia(ctx context.Context, params protocol.PreflightMediaParams) (protocol.MediaInfo, error) {
	return call[protocol.MediaInfo](c, ctx, "preflight_media", params, nil)
}

func (c *Client) GetMarkers(ctx context.Context) (protocol.MarkersResult, error) {
	return call[protocol.MarkersResult](c, ctx, "get_markers", struct{}{}, nil)
}

func (c *Client) GetBPMGrid(ctx context.Context) (protocol.BPMGridResult, error) {
	return call[protocol.BPMGridResult](c, ctx, "get_bpm_grid", struct{}{}, nil)
}

func (c *Client) Diagnostics(ctx context.Context) (protocol.DiagnosticsResult, error) {
	return call[protocol.DiagnosticsResult](c, ctx, "diagnostics", struct{}{}, nil)
}

func (c *Client) AddText(ctx context.Context, params protocol.AddTextParams, expected *protocol.ExpectedContext) (protocol.MutationResult, error) {
	return call[protocol.MutationResult](c, ctx, "add_text", params, expected)
}

func (c *Client) AddMedia(ctx context.Context, params protocol.AddMediaParams, expected *protocol.ExpectedContext) (protocol.MutationResult, error) {
	return call[protocol.MutationResult](c, ctx, "add_media", params, expected)
}

func (c *Client) UpdateObject(ctx context.Context, params protocol.UpdateObjectParams, expected *protocol.ExpectedContext) (protocol.MutationResult, error) {
	return call[protocol.MutationResult](c, ctx, "update_object", params, expected)
}

func (c *Client) DeleteObject(ctx context.Context, objectID uint64, expected *protocol.ExpectedContext) (protocol.MutationResult, error) {
	return call[protocol.MutationResult](c, ctx, "delete_object", protocol.DeleteObjectParams{ObjectID: objectID}, expected)
}

func (c *Client) MutateEffect(ctx context.Context, method string, params protocol.EffectMutationParams, expected *protocol.ExpectedContext) (protocol.MutationResult, error) {
	return call[protocol.MutationResult](c, ctx, method, params, expected)
}

func (c *Client) ExecuteBatch(ctx context.Context, params protocol.ExecuteBatchParams, expected *protocol.ExpectedContext) (protocol.MutationResult, error) {
	return call[protocol.MutationResult](c, ctx, "execute_batch", params, expected)
}

func (c *Client) RenderPreview(ctx context.Context, params protocol.RenderPreviewParams) (protocol.PreviewResult, error) {
	return call[protocol.PreviewResult](c, ctx, "render_preview", params, nil)
}

func call[T any](c *Client, ctx context.Context, method string, params any, expected *protocol.ExpectedContext) (T, error) {
	var result T
	err := c.call(ctx, method, params, expected, &result)
	return result, err
}

func (c *Client) call(ctx context.Context, method string, params any, expected *protocol.ExpectedContext, result any) (returnErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	started := time.Now()
	defer func() {
		entry := CallLog{Time: started, Method: method, DurationMS: time.Since(started).Milliseconds()}
		if returnErr != nil {
			entry.Error = returnErr.Error()
		}
		c.history = append(c.history, entry)
		if len(c.history) > 50 {
			c.history = append([]CallLog(nil), c.history[len(c.history)-50:]...)
		}
	}()

	if err := c.connectLocked(ctx); err != nil {
		return err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode bridge params: %w", err)
	}
	c.nextID++
	req := protocol.Request{ID: c.nextID, Version: protocol.Version, Method: method, Params: paramsJSON, Context: expected}
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode bridge request: %w", err)
	}

	deadline := time.Now().Add(c.timeout)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		_ = c.closeLocked()
		return err
	}
	if err := protocol.WriteFrame(c.conn, payload); err != nil {
		_ = c.closeLocked()
		return fmt.Errorf("write bridge request: %w", err)
	}
	responsePayload, err := protocol.ReadFrame(c.conn)
	if err != nil {
		_ = c.closeLocked()
		return fmt.Errorf("read bridge response: %w", err)
	}

	var response protocol.Response
	if err := json.Unmarshal(responsePayload, &response); err != nil {
		_ = c.closeLocked()
		return fmt.Errorf("decode bridge response: %w", err)
	}
	if response.ID != req.ID {
		_ = c.closeLocked()
		return fmt.Errorf("bridge response id mismatch: got %d, want %d", response.ID, req.ID)
	}
	if response.Version != protocol.Version {
		return fmt.Errorf("bridge protocol version mismatch: got %d, want %d", response.Version, protocol.Version)
	}
	if response.Error != nil {
		return &RemoteError{Code: response.Error.Code, Message: response.Error.Message, Retryable: response.Error.Retryable, Details: response.Error.Details}
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
