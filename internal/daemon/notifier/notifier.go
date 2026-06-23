// Package notifier adapts an *rpc.Conn to handlers.NotifierPort so chat /
// approval handlers can send notifications and reverse-direction requests
// to the connected client without depending on the websocket transport.
package notifier

import (
	"context"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// Notifier wraps a single *rpc.Conn.
type Notifier struct{ conn *rpc.Conn }

func New(conn *rpc.Conn) *Notifier { return &Notifier{conn: conn} }

func (n *Notifier) Notify(method string, params any) error {
	return n.conn.Notify(method, params)
}

func (n *Notifier) Request(ctx context.Context, method string, params any, result any) error {
	return n.conn.Call(ctx, method, params, result)
}
