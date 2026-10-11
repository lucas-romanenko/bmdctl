// SPDX-License-Identifier: MIT

package main

// bmdctl mcp: the MCP server (internal/mcp) on stdin and stdout, its
// requests sent to the engine, started if none runs. Files a tool sends
// or writes are this machine's, as the CLI's are.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/mcp"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

const mcpUsage = `usage: bmdctl mcp

Runs a Model Context Protocol server on stdin and stdout, for an AI
agent: a tool per command (atem_program, atem_usk_on_air, hyperdeck_play,
...), with list_hosts, get_state and list_inputs, and each host's state
as a resource it can subscribe to. Commands go through the engine,
started if none runs, as any command's do. Destructive tools do nothing
without yes: true and say what they would affect, for the agent to ask
a person first. docs/AGENTS.md says how to add it to Claude Code and
Claude Desktop.
`

// mcpConfig is the MCP server wired to this CLI.
func (c *cli) mcpConfig(g globals) mcp.Config {
	return mcp.Config{
		Registry: c.reg,
		Call: func(ctx context.Context, req engine.Request, onFrame func(engine.Frame) error) error {
			err := c.callCtx(ctx, req, nil, onFrame)
			var ee *errExit
			if errors.As(err, &ee) {
				return &engine.Error{Code: codeName(ee.code), Err: ee.err}
			}
			return err
		},
		Hosts: func() (*hosts.File, error) {
			f, _, err := c.loadHosts()
			return f, err
		},
		Target: func(spec string) (mcp.Target, error) {
			tg := g
			if spec != "" {
				tg.host, tg.hostSet = spec, true
			}
			t, err := c.resolveTarget(tg)
			if err != nil {
				return mcp.Target{}, err
			}
			return mcp.Target{Address: t.Address, Device: t.Device, Label: t.label()}, nil
		},
		ReadFile: os.ReadFile,
		WriteFile: func(e *table.Entry, params map[string]string, f engine.FileFrame) (string, error) {
			path, err := outPath(e, params, f)
			if err != nil {
				return "", err
			}
			if dir := filepath.Dir(path); dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
			}
			return path, os.WriteFile(path, f.Data, 0o644)
		},
		Version: shortVersion(),
		Logger:  slog.New(slog.NewTextHandler(c.stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
}

func (c *cli) mcpCommand(g globals, words, tokens []string) int {
	if g.help {
		fmt.Fprint(c.stdout, mcpUsage)
		return exitOK
	}
	if len(words) > 0 || len(tokens) > 0 {
		return c.fail(exitUsage, "bmdctl mcp: takes nothing (it speaks on stdin and stdout)")
	}
	if err := mcp.New(c.mcpConfig(g)).Serve(c.ctx, c.stdin, c.stdout); err != nil && c.ctx.Err() == nil {
		fmt.Fprintf(c.stderr, "bmdctl mcp: %v\n", err)
		return exitUsage
	}
	return exitOK
}
