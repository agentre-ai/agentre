package rpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Subprotocol is the canonical WebSocket subprotocol advertised by the
// daemon and matched by the client.
const Subprotocol = "agentred-jsonrpc.v1"

// LANOpts configures the LAN-mode transport.
type LANOpts struct {
	Host        string
	Port        int
	TLSCertFile string
	TLSKeyFile  string
	Registry    *Registry
	// OnConn is invoked once per accepted connection so daemon.go can attach
	// per-connection state (notifier, auth bindings) before Serve starts.
	OnConn func(*Conn)
}

// LANServer accepts WebSocket connections at /rpc and runs one *Conn per
// peer through the registry.
type LANServer struct {
	opts LANOpts

	mu       sync.Mutex
	listener net.Listener
	srv      *http.Server
}

// NewLANServer creates a new LANServer with the given options.
func NewLANServer(opts LANOpts) *LANServer { return &LANServer{opts: opts} }

// Run starts the server and blocks until ctx is canceled or a fatal error
// occurs. It returns nil if the server was shut down cleanly.
func (s *LANServer) Run(ctx context.Context) error {
	if (s.opts.TLSCertFile == "") != (s.opts.TLSKeyFile == "") {
		return fmt.Errorf("tls: both --tls-cert and --tls-key must be set or neither")
	}
	addr := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{
		Subprotocols: []string{Subprotocol},
		CheckOrigin:  func(r *http.Request) bool { return true },
	}
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := NewConn(ws, s.opts.Registry)
		if s.opts.OnConn != nil {
			s.opts.OnConn(c)
		}
		// 用 Run(ctx) 的 daemon 主 ctx，不用 r.Context() —— 后者在 hijack
		// 后 handler 一返回就被 net/http cancel，从而让 Serve 派发的所有
		// chat.start 等 RPC handler 一开 ctx 就是 context.Canceled。
		go c.Serve(ctx)
	})
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	//nolint:gosec // G118: shutdown must use a fresh ctx since the request ctx is already canceled
	go func() {
		<-ctx.Done()
		_ = s.srv.Shutdown(context.Background())
	}()
	if s.opts.TLSCertFile != "" {
		// Validate the cert/key pair early for clean errors.
		if _, err := tls.LoadX509KeyPair(s.opts.TLSCertFile, s.opts.TLSKeyFile); err != nil {
			return fmt.Errorf("tls: %w", err)
		}
		err = s.srv.ServeTLS(ln, s.opts.TLSCertFile, s.opts.TLSKeyFile)
	} else {
		err = s.srv.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Addr returns the bound "host:port" after Run starts listening.
func (s *LANServer) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// URL returns the canonical ws[s]://addr/rpc endpoint.
func (s *LANServer) URL() string {
	scheme := "ws"
	if s.opts.TLSCertFile != "" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s/rpc", scheme, s.Addr())
}
