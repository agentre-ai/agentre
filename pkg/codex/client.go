package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/agents/provider"
)

type Client struct {
	binary       string
	cwd          string
	env          map[string]string
	model        string
	systemPrompt string
	sandbox      SandboxMode
	approval     ApprovalPolicy
	config       []string
	extraArgs    []string
	killGrace    time.Duration
	runner       appServerRunner
}

func New(opts ...Option) *Client {
	c := &Client{
		binary:    "codex",
		approval:  ApprovalNever,
		killGrace: 10 * time.Second,
		runner:    execAppServerRunner{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) Stream(ctx context.Context, prompt string, opts ...RunOption) (*Stream, error) {
	spec := c.defaultRunSpec()
	for _, o := range opts {
		o(&spec)
	}
	app, err := c.startApp(ctx)
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		_ = app.terminate(context.Background(), c.killGrace)
	}
	if err := initializeApp(ctx, app); err != nil {
		cleanup()
		return nil, err
	}
	thread, err := c.startOrResumeThread(ctx, app, spec)
	if err != nil {
		cleanup()
		return nil, err
	}
	turnParams, err := turnStartParams(thread, prompt, spec.collaborationMode, c.model)
	if err != nil {
		cleanup()
		return nil, err
	}
	raw, err := app.Call(ctx, appMethodTurnStart, turnParams)
	if err != nil {
		cleanup()
		return nil, err
	}
	var turn appTurnStartResponse
	if err := json.Unmarshal(raw, &turn); err != nil {
		cleanup()
		return nil, err
	}
	if turn.Turn.ID == "" {
		cleanup()
		return nil, errors.New("codex: turn/start response missing id")
	}
	stream := newStream(app, c.killGrace, thread.ThreadID, turn.Turn.ID, "")
	go stream.drain(ctx)
	return stream, nil
}

func (c *Client) Compact(ctx context.Context, threadID string) (*Stream, error) {
	if strings.TrimSpace(threadID) == "" {
		return nil, errors.New("codex: compact thread id is required")
	}
	app, err := c.startApp(ctx)
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		_ = app.terminate(context.Background(), c.killGrace)
	}
	if err := initializeApp(ctx, app); err != nil {
		cleanup()
		return nil, err
	}
	thread, err := c.startOrResumeThread(ctx, app, runSpec{
		resumeID: strings.TrimSpace(threadID),
		cwd:      c.cwd,
		sandbox:  c.sandbox,
		approval: c.approval,
	})
	if err != nil {
		cleanup()
		return nil, err
	}
	if _, err := app.Call(ctx, appMethodThreadCompact, map[string]any{
		"threadId": thread.ThreadID,
	}); err != nil {
		cleanup()
		return nil, err
	}
	stream := newStream(app, c.killGrace, thread.ThreadID, "", "manual")
	go stream.drain(ctx)
	return stream, nil
}

func (c *Client) Text(ctx context.Context, prompt string, opts ...RunOption) (string, error) {
	stream, err := c.Stream(ctx, prompt, opts...)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	var stopErr error
	for stream.Next() {
		ev := stream.Event()
		switch ev.Kind {
		case EventTextDelta:
			b.WriteString(ev.Text)
		case EventError:
			if ev.Err != nil {
				stopErr = ev.Err
			}
		}
	}
	if err := stream.Close(ctx); err != nil && stopErr == nil {
		stopErr = err
	}
	if stopErr != nil {
		return "", stopErr
	}
	return b.String(), nil
}

type ForkThreadResult struct {
	ThreadID     string
	ForkedFromID string
}

func (c *Client) ForkThread(ctx context.Context, sourceThreadID string) (*ForkThreadResult, error) {
	spec := c.defaultRunSpec()
	app, err := c.startApp(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = app.terminate(context.Background(), c.killGrace) }()
	if err := initializeApp(ctx, app); err != nil {
		return nil, err
	}
	params := threadParams(c, spec)
	params["threadId"] = sourceThreadID
	raw, err := app.Call(ctx, appMethodThreadFork, params)
	if err != nil {
		return nil, err
	}
	var res appThreadResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.Thread.ID == "" {
		return nil, errors.New("codex: thread/fork response missing id")
	}
	return &ForkThreadResult{ThreadID: res.Thread.ID, ForkedFromID: res.Thread.ForkedFromID}, nil
}

type RollbackThreadResult struct {
	ThreadID string
}

func (c *Client) RollbackThread(ctx context.Context, threadID string, numTurns int) (*RollbackThreadResult, error) {
	if numTurns <= 0 {
		return nil, errors.New("codex: thread/rollback numTurns must be >= 1")
	}
	app, err := c.startApp(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = app.terminate(context.Background(), c.killGrace) }()
	if err := initializeApp(ctx, app); err != nil {
		return nil, err
	}
	if _, err := c.startOrResumeThread(ctx, app, runSpec{resumeID: threadID, cwd: c.cwd, sandbox: c.sandbox, approval: c.approval}); err != nil {
		return nil, err
	}
	raw, err := app.Call(ctx, appMethodThreadRollback, map[string]any{
		"threadId": threadID,
		"numTurns": numTurns,
	})
	if err != nil {
		return nil, err
	}
	var res appThreadResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.Thread.ID == "" {
		return nil, errors.New("codex: thread/rollback response missing id")
	}
	return &RollbackThreadResult{ThreadID: res.Thread.ID}, nil
}

func (c *Client) Close(_ context.Context) error { return nil }

func (c *Client) defaultRunSpec() runSpec {
	return runSpec{
		cwd:      c.cwd,
		sandbox:  c.sandbox,
		approval: c.approval,
	}
}

func (c *Client) startApp(ctx context.Context) (*appClient, error) {
	return newAppClient(ctx, c.runner, procOptions{
		Binary: c.binary,
		Args:   buildAppServerArgs(c.config, c.extraArgs),
		Cwd:    c.cwd,
		Env:    buildEnv(c.env),
	})
}

func initializeApp(ctx context.Context, app *appClient) error {
	if _, err := app.Call(ctx, appMethodInitialize, map[string]any{
		"clientInfo": map[string]any{
			"name":    "agentre",
			"title":   "Agentre",
			"version": "0.0.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return err
	}
	return app.Notify(ctx, appMethodInitialized, nil)
}

func (c *Client) startOrResumeThread(ctx context.Context, app *appClient, spec runSpec) (appThreadStartResult, error) {
	params := threadParams(c, spec)
	method := appMethodThreadStart
	if spec.resumeID != "" {
		method = appMethodThreadResume
		params["threadId"] = spec.resumeID
		params["excludeTurns"] = true
	}
	raw, err := app.Call(ctx, method, params)
	if err != nil {
		return appThreadStartResult{}, err
	}
	var res appThreadResponse
	if err := json.Unmarshal(raw, &res); err != nil {
		return appThreadStartResult{}, err
	}
	if res.Thread.ID != "" {
		return appThreadStartResult{ThreadID: res.Thread.ID, Model: res.Model}, nil
	}
	return appThreadStartResult{}, errors.New("codex: thread response missing id")
}

type Stream struct {
	app       *appClient
	killGrace time.Duration
	events    chan Event

	mu            sync.RWMutex
	sessionID     string
	turnID        string
	cur           Event
	err           error
	usage         provider.Usage
	contextWindow int

	userInputMu       sync.Mutex
	userInputRequests map[string]json.RawMessage
	compactSeen       map[string]struct{}
	compactTrigger    string

	closeOnce sync.Once
}

func newStream(app *appClient, killGrace time.Duration, threadID, turnID, compactTrigger string) *Stream {
	return &Stream{
		app:               app,
		killGrace:         killGrace,
		events:            make(chan Event, 64),
		sessionID:         threadID,
		turnID:            turnID,
		userInputRequests: map[string]json.RawMessage{},
		compactSeen:       map[string]struct{}{},
		compactTrigger:    compactTrigger,
	}
}

func (s *Stream) Next() bool {
	ev, ok := <-s.events
	if !ok {
		return false
	}
	s.cur = ev
	return true
}

func (s *Stream) Event() Event { return s.cur }

func (s *Stream) SessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func (s *Stream) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

func (s *Stream) Close(ctx context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		err = s.app.terminate(ctx, s.killGrace)
	})
	if err != nil {
		return err
	}
	return s.Err()
}
