package signaling

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
)

// Client wraps a WebSocket connection to the relay with JSON envelope I/O.
type Client struct {
	conn *websocket.Conn
}

// Dial connects to the relay WebSocket endpoint.
func Dial(ctx context.Context, url string) (*Client, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dial relay: %w", err)
	}
	return &Client{conn: conn}, nil
}

// FromConn wraps an already-accepted WebSocket connection.
// Used on the relay side to share the same Send/Read helpers as the dial side.
func FromConn(conn *websocket.Conn) *Client {
	return &Client{conn: conn}
}

// Send marshals payload as JSON and writes an Envelope to the wire.
func (c *Client) Send(ctx context.Context, msgType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	env := Envelope{Type: msgType, Payload: raw}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// Read reads the next Envelope from the wire. Use DecodePayload to unmarshal
// the typed payload after dispatching on envelope.Type.
func (c *Client) Read(ctx context.Context) (*Envelope, error) {
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}
	return &env, nil
}

// DecodePayload unmarshals envelope.Payload into the provided typed value.
func DecodePayload(env *Envelope, out any) error {
	return json.Unmarshal(env.Payload, out)
}

// Close shuts the connection down cleanly.
func (c *Client) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

// Conn returns the underlying WebSocket connection.
// Useful for the relay-side handler that needs to read/write on a conn it didn't dial.
func (c *Client) Conn() *websocket.Conn {
	return c.conn
}
