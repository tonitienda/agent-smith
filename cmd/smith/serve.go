package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/tonitienda/agent-smith/internal/cli"
	"github.com/tonitienda/agent-smith/internal/command"
	"github.com/tonitienda/agent-smith/internal/cost"
	"github.com/tonitienda/agent-smith/internal/delegate"
	"github.com/tonitienda/agent-smith/internal/hook"
	"github.com/tonitienda/agent-smith/internal/loop"
	"github.com/tonitienda/agent-smith/internal/permission"
	"github.com/tonitienda/agent-smith/internal/routing"
	"github.com/tonitienda/agent-smith/internal/serve"
	"github.com/tonitienda/agent-smith/internal/session"
	"github.com/tonitienda/agent-smith/internal/subagent"
	"github.com/tonitienda/agent-smith/internal/tool"
	"github.com/tonitienda/agent-smith/internal/tool/builtin"
)

// serveCommand starts the local JSON-RPC/WebSocket session server (AS-077): the
// programmatic spine the graphical faces (web GUI AS-078, Viscose AS-081) drive.
// It binds loopback by default; a non-loopback bind requires --unsafe-bind and
// emits the AS-080 caveat, since serve is the single-user local daemon, not a
// multi-tenant sandbox (D9: "not a sandbox").
func serveCommand() *cli.Command {
	var addr string
	var unsafeBind bool
	return &cli.Command{
		Name:          "serve",
		Summary:       "Start a local JSON-RPC/WebSocket session server",
		Usage:         "[--addr host:port]",
		Scriptability: command.Scriptable.String(),
		Reason:        "long-running local daemon; clients (web GUI, editor) connect over WebSocket",
		Examples: []string{
			"smith serve",
			"smith serve --addr 127.0.0.1:7777",
		},
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&addr, "addr", "127.0.0.1:8765", "listen address (loopback by default)")
			fs.BoolVar(&unsafeBind, "unsafe-bind", false, "allow binding a non-loopback address (AS-080: serve is single-user, not a sandbox)")
		},
		Run: func(c *cli.Context) error { return runServe(c, addr, unsafeBind) },
	}
}

// runServe builds the backend and serves until interrupted.
func runServe(c *cli.Context, addr string, unsafeBind bool) error {
	if err := checkBind(addr, unsafeBind, c.Stderr); err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	store, err := session.NewStore("", wd)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	backend := &serveBackend{override: c.Globals.Config, wd: wd, store: store, stderr: c.Stderr}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	srv := serve.NewServer(backend, serve.WithErrorLog(func(format string, a ...any) {
		_, _ = fmt.Fprintf(c.Stderr, "serve: "+format+"\n", a...)
	}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if !c.Globals.Quiet {
		_, _ = fmt.Fprintf(c.Stderr, "smith serve listening on ws://%s (localhost only; Ctrl+C to stop)\n", ln.Addr())
	}
	return srv.Serve(ctx, ln)
}

// checkBind enforces the loopback-by-default posture (AC6). An explicit loopback
// host is always allowed; any other (including the all-interfaces empty host)
// requires --unsafe-bind and prints the AS-080 caveat.
func checkBind(addr string, unsafe bool, stderr io.Writer) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return cli.Usagef("serve: invalid --addr %q: %v", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if !unsafe {
		return cli.Usagef("serve: refusing to bind non-loopback address %q without --unsafe-bind (AS-080: serve is single-user and not a sandbox)", host)
	}
	_, _ = fmt.Fprintf(stderr, "warning: binding non-loopback %q — serve is single-user and NOT a sandbox (AS-080); it exposes shell, filesystem, and key access to anyone who can reach this port\n", host)
	return nil
}

// serveBackend implements serve.Backend by reusing the headless loop wiring
// (AS-051): each connection gets a session and an engine built the same way a
// `smith run` does, with the permission ask-mode forwarded to the client.
type serveBackend struct {
	override string
	wd       string
	store    *session.Store
	stderr   io.Writer
}

func (b *serveBackend) List() ([]serve.SessionInfo, error) {
	sums, err := b.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]serve.SessionInfo, 0, len(sums))
	for _, s := range sums {
		out = append(out, serve.SessionInfo{
			ID:         s.ID,
			Title:      s.Title,
			UpdatedAt:  s.UpdatedAt.Format(time.RFC3339),
			EventCount: s.EventCount,
		})
	}
	return out, nil
}

func (b *serveBackend) Open(resumeID string, conn serve.Conn) (serve.Session, error) {
	var (
		sess *session.Session
		err  error
	)
	if resumeID != "" {
		sess, err = b.store.Open(resumeID)
	} else {
		sess, err = b.store.Create("")
	}
	if err != nil {
		return nil, err
	}
	// A fresh session is seeded with the project memory files, just like the TUI
	// and headless faces; a resumed session already carries its history.
	if resumeID == "" {
		if err := seedMemory(b.wd, sess); err != nil {
			_ = sess.Log.Close()
			return nil, err
		}
	}

	tools, err := appRuntime.BuiltinTools(b.wd)
	if err != nil {
		_ = sess.Log.Close()
		return nil, err
	}
	prov, provName, model, err := headlessProvider(b.override)
	if err != nil {
		_ = sess.Log.Close()
		return nil, err
	}
	pricing, err := sessionPricing(b.override)
	if err != nil {
		_ = sess.Log.Close()
		return nil, fmt.Errorf("load pricing table: %w", err)
	}
	cfg, err := loadLayeredConfig(b.override)
	if err != nil {
		_ = sess.Log.Close()
		return nil, fmt.Errorf("load config: %w", err)
	}
	hooks := loadHooks(cfg, b.stderr)

	// Permission gate (AS-016): the merged config posture with the connected client
	// as the Asker. An ask-mode call is forwarded over the wire; a client that
	// cannot answer surfaces an error, which the policy treats as a denial — fail
	// fast, never hang (D-CLI-9 parity for a programmatic face).
	permCfg, err := mergedPermissionConfig(b.wd, b.override)
	if err != nil {
		_ = sess.Log.Close()
		return nil, fmt.Errorf("load permission policy: %w", err)
	}
	policy := permission.New(permCfg, permission.WithAsker(serveAsker{conn: conn}))

	// User-delegated subagents (AS-046/AS-119): a serve session exposes the `task`
	// tool with the same isolation, linking, and cost rollup as the TUI. The child
	// inherits this session's gate, so a child tool call in ask mode is forwarded to
	// the connected client (never a hang). A serve session loads no skills or MCP
	// servers, so the child gets the builtin tool set.
	router, _ := routing.ConfigFrom(cfg)
	taskParent := func() delegate.Parent {
		return delegate.Parent{
			Log:        sess.Log,
			SessionID:  sess.ID,
			ProvName:   provName,
			Model:      model,
			Permission: policy.Func(),
			Router:     router,
		}
	}
	if err := tools.Register(builtin.NewTask(taskSpawner(b.store, b.wd, nil, nil, taskParent))); err != nil {
		_ = sess.Log.Close()
		return nil, fmt.Errorf("register task tool: %w", err)
	}

	rtOpts := append([]tool.Option{tool.WithPermission(policy.Func())}, hookToolOptions(hooks, sess.Log, sess.ID)...)
	rt := tool.NewRuntime(tools, sess.Log, rtOpts...)

	observer := func(ev loop.UIEvent) { conn.Emit(mapEvent(ev)) }
	engOpts := []loop.Option{loop.WithObserver(observer)}

	// Sub-agent lifecycle (AS-107) parity with headless: the durable fact ledger is
	// shared with other sessions of this project. A serve session does not load the
	// skill tool, so the resolver only needs the working-directory memory tree.
	subReg, subStore, err := buildSubAgents(cfg, b.store, saveTargetResolver(b.wd, nil), openFactLedger(b.store, b.stderr), nil, b.stderr)
	if err != nil {
		_ = sess.Log.Close()
		return nil, fmt.Errorf("build sub-agents: %w", err)
	}
	engOpts = append(engOpts, loop.WithSubAgents(subagent.NewRunner(subReg, subStore, sess.ID)))

	eng, err := loop.New(prov, sess.Log, rt, tools, model, engOpts...)
	if err != nil {
		_ = sess.Log.Close()
		return nil, fmt.Errorf("build engine: %w", err)
	}

	fireLifecycle(context.Background(), hooks, sess.Log, hook.Payload{Event: hook.SessionStart, Session: sess.ID})
	return &serveSession{sess: sess, eng: eng, pricing: pricing, hooks: hooks}, nil
}

// serveSession drives one connection's session: the same engine across turns
// (multi-turn parity with the TUI), priced against the same table as /cost.
type serveSession struct {
	sess    *session.Session
	eng     *loop.Engine
	pricing *cost.Table
	hooks   *hook.Set
}

func (s *serveSession) ID() string { return s.sess.ID }

func (s *serveSession) Run(ctx context.Context, prompt string) (serve.Result, error) {
	// Honor the user-prompt-submit hook (AS-035) as the other faces do: it may
	// block the turn or rewrite the prompt the model receives.
	out := fireLifecycle(ctx, s.hooks, s.sess.Log, hook.Payload{Event: hook.UserPromptSubmit, Session: s.sess.ID, Prompt: prompt})
	if out.Blocked {
		return serve.Result{}, fmt.Errorf("prompt blocked by hook: %s", out.Reason)
	}
	if rewritten := promptRewrite(out.Input); rewritten != "" {
		prompt = rewritten
	}
	res, err := s.eng.Run(ctx, prompt)
	totalUSD := cost.Summarize(s.sess.Log.Events(), s.pricing).TotalUSD
	return serve.Result{
		Text:       res.FinalText,
		SessionID:  s.sess.ID,
		StopReason: res.StopReason,
		CostUSD:    totalUSD,
		Iterations: res.Iterations,
	}, err
}

func (s *serveSession) Close() error {
	fireLifecycle(context.Background(), s.hooks, s.sess.Log, hook.Payload{Event: hook.SessionStop, Session: s.sess.ID})
	return s.sess.Log.Close()
}

// serveAsker bridges the permission Asker (AS-016) to the connected client: an
// ask-mode prompt is forwarded over the wire and the client's answer gates the
// call. A transport error is returned, which the Policy treats as a denial.
type serveAsker struct{ conn serve.Conn }

func (a serveAsker) Ask(ctx context.Context, req permission.Request) (permission.Outcome, error) {
	dec, err := a.conn.AskPermission(ctx, serve.PermissionRequest{
		Tool:      req.Tool,
		Subject:   req.Subject,
		Arguments: req.Arguments,
	})
	if err != nil {
		return permission.Outcome{}, err
	}
	return permission.Outcome{Allow: dec.Allow}, nil
}

// mapEvent flattens a loop UIEvent onto the wire Event, mirroring the headless
// stream-json mapping (one event substrate, many renderers).
func mapEvent(ev loop.UIEvent) serve.Event {
	se := serve.Event{
		Type:       string(ev.Kind),
		Iteration:  ev.Iteration,
		Text:       ev.Text,
		StopReason: ev.StopReason,
		SpentUSD:   ev.BudgetSpentUSD,
		LimitUSD:   ev.BudgetLimitUSD,
	}
	if ev.Tool != nil {
		se.Tool = ev.Tool.Name
	}
	return se
}
