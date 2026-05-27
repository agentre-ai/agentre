// Package codex is Agentre's local wrapper for the OpenAI Codex CLI app-server.
//
// Scope: spawn `codex app-server --listen stdio://`, speak its line-delimited
// JSON-RPC protocol, and expose the small event surface Agentre consumes:
// text deltas, thinking deltas, tool lifecycle events, usage, done, errors,
// thread resume, and thread fork.
package codex
