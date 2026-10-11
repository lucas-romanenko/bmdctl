// SPDX-License-Identifier: MIT

package main

// bmdctl serve: the HTTP/JSON API (internal/serve) in the foreground, its
// requests sent to the engine, started if none runs. bmdctl engine
// --serve runs it in the engine's own process (enginecmd.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/serve"
)

const serveUsage = `usage: bmdctl serve [--listen ADDRESS]

Serves every command over HTTP/JSON, through the engine (started if none
runs), until interrupted: GET /hosts, GET /hosts/{host}/state,
POST /hosts/{host}/{command} with the arguments as a JSON object
(multipart/form-data for a file), GET /hosts/{host}/events (server-sent
events, watch's frames) and GET /openapi.json, which describes it all.
An answer is the engine's, as --json prints it.

--listen is 127.0.0.1:8880 unless given. There is no authentication:
anyone who can reach the address controls the devices, so it listens on
this machine only unless told otherwise (WebATEM's posture).
'bmdctl engine --serve' serves it from the engine's own process.
`

// apiConfig is the API wired to this CLI: requests go to the engine,
// hosts come from hosts.yml.
func (c *cli) apiConfig(logLine func(method, path string, status int, took time.Duration)) serve.Config {
	return serve.Config{
		Registry: c.reg,
		Call: func(ctx context.Context, req engine.Request, onFrame func(engine.Frame) error) error {
			return c.callCtx(ctx, req, nil, onFrame)
		},
		Hosts: func() (*hosts.File, error) {
			f, _, err := c.loadHosts()
			return f, err
		},
		Version: shortVersion(),
		Log:     logLine,
	}
}

// listenFlag reads --listen from a built-in's flag tokens.
func listenFlag(cmd string, tokens []string) (listen string, given bool, rest []string, err error) {
	listen = serve.DefaultListen
	for i := 0; i < len(tokens); i++ {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(tokens[i], "--"), "=")
		if name != "listen" {
			rest = append(rest, tokens[i])
			continue
		}
		if !hasValue {
			if i+1 == len(tokens) {
				return "", false, nil, fmt.Errorf("bmdctl %s: --listen needs an address", cmd)
			}
			i++
			value = tokens[i]
		}
		if _, _, err := net.SplitHostPort(value); err != nil {
			return "", false, nil, fmt.Errorf("bmdctl %s: --listen %q: not host:port", cmd, value)
		}
		listen, given = value, true
	}
	return listen, given, rest, nil
}

// loopback reports whether an address is this machine's alone.
func loopback(addr string) bool {
	h, _, _ := net.SplitHostPort(addr)
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (c *cli) serveCommand(g globals, words, tokens []string) int {
	if g.help {
		fmt.Fprint(c.stdout, serveUsage)
		return exitOK
	}
	if len(words) > 0 {
		return c.fail(exitUsage, "bmdctl serve: unexpected %q", words[0])
	}
	listen, _, rest, err := listenFlag("serve", tokens)
	if err != nil {
		return c.fail(exitUsage, "%v", err)
	}
	if len(rest) > 0 {
		return c.fail(exitUsage, "bmdctl serve: unexpected %q", rest[0])
	}
	stop, err := c.startAPI(g, listen)
	if err != nil {
		return c.fail(exitUsage, "bmdctl serve: %v", err)
	}
	<-c.ctx.Done()
	stop()
	return exitOK
}

// startAPI serves the API on listen until stop is called, saying so,
// and logging each request for people.
func (c *cli) startAPI(g globals, listen string) (stop func(), err error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex // access lines from concurrent requests, whole
	logLine := func(method, path string, status int, took time.Duration) {
		if g.quiet || g.json {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if c.terminal() {
			row := indent + c.colour(dim, c.now().Format("15:04:05")) + "  " + padRight(method, 5) + "  " + path
			fmt.Fprintln(c.stdout, trailing(row, c.colour(dim, fmt.Sprintf("%d · %s", status, tookText(took)))))
			return
		}
		fmt.Fprintf(c.stdout, "%s %s %s %d %d ms\n", c.now().Format("15:04:05"), method, path, status, took.Milliseconds())
	}
	srv := &http.Server{Handler: serve.Handler(c.apiConfig(logLine)), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	url := "http://" + ln.Addr().String()
	switch {
	case g.quiet:
	case g.json:
		b, _ := json.Marshal(engine.Response{OK: true, Result: map[string]string{"listening": url}})
		fmt.Fprintf(c.stdout, "%s\n", b)
	case c.terminal():
		fmt.Fprintln(c.stdout, c.colour(bold, "bmdctl serve")+"  "+c.colour(dim, url+" · ctrl-c to stop"))
		fmt.Fprintln(c.stdout, indent+c.colour(dim, "no authentication: whoever reaches it controls the devices"))
	default:
		fmt.Fprintf(c.stdout, "serving %s (no authentication)\n", url)
	}
	if !loopback(ln.Addr().String()) && !g.quiet {
		c.notice("%s is reachable from other machines, with no authentication: anyone who can reach it controls the devices", url)
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srv.Close()
		}
	}, nil
}

// tookText is a request's time as an access line shows it.
func tookText(d time.Duration) string { return took(d.Milliseconds()) }
