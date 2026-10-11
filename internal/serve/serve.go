// SPDX-License-Identifier: MIT

// Package serve is bmdctl's HTTP/JSON API (bmdctl serve, bmdctl engine
// --serve): the command table's commands, run by the engine, over HTTP.
//
//	GET  /hosts                       hosts.yml, as bmdctl hosts --json prints it
//	GET  /hosts/{host}/state          the device's state (?path=mes.0)
//	GET  /hosts/{host}/events         its changes as server-sent events, watch's frames
//	POST /hosts/{host}/{command...}   a command: /hosts/studio/program, {"source": "cam2"}
//	GET  /openapi.json                all of it, generated from the table
//
// A command's arguments are a JSON object, or multipart/form-data for a
// file it sends; ?yes=true allows a destructive one; ?timeout and ?fps
// are the CLI's flags. The answer is the engine's, exactly as --json
// prints it, with "files" added for what a command writes and "confirm"
// for what a destructive one refused would have affected. There is no
// authentication: it binds to 127.0.0.1 unless told otherwise, as
// WebATEM does. docs/PORTING.md, "serve", records the decisions.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/engine"
	"github.com/lucas-romanenko/bmdctl/internal/hosts"
	"github.com/lucas-romanenko/bmdctl/internal/table"
)

// DefaultListen is where serve listens unless told otherwise: this
// machine only.
const DefaultListen = "127.0.0.1:8880"

// Config is what the API needs.
type Config struct {
	Registry *table.Registry
	// Call sends a request to the engine and passes each frame of its
	// reply to onFrame, until the reply ends or ctx does.
	Call func(ctx context.Context, req engine.Request, onFrame func(engine.Frame) error) error
	// Hosts reads hosts.yml.
	Hosts func() (*hosts.File, error)
	// Version is bmdctl's, for the OpenAPI document.
	Version string
	// Log, if set, is called once per request with what was asked and
	// the status answered.
	Log func(method, path string, status int, took time.Duration)
	// Logger receives errors.
	Logger *slog.Logger
}

// maxBody bounds a request's body: a still or a profile with its images.
const maxBody = 256 << 20

type api struct{ cfg Config }

// Handler serves the API.
func Handler(cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	a := &api{cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.index)
	mux.HandleFunc("GET /openapi.json", a.openapi)
	mux.HandleFunc("GET /hosts", a.hosts)
	mux.HandleFunc("GET /hosts/{host}/state", a.state)
	mux.HandleFunc("GET /hosts/{host}/events", a.events)
	mux.HandleFunc("POST /hosts/{host}/{command...}", a.command)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, engine.CodeUsage, fmt.Sprintf("no %s %s: GET /openapi.json lists what there is", r.Method, r.URL.Path))
	})
	return logged(cfg, mux)
}

// logged reports each request to cfg.Log.
func logged(cfg Config, h http.Handler) http.Handler {
	if cfg.Log == nil {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		cfg.Log(r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Status is the HTTP status for an engine error code.
func Status(code string) int {
	switch code {
	case "":
		return http.StatusOK
	case engine.CodeUsage:
		return http.StatusBadRequest
	case engine.CodeDestructive:
		return http.StatusConflict
	case engine.CodeRejected:
		return http.StatusUnprocessableEntity
	case engine.CodeNoEcho:
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway // unreachable, engine, stale
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(v)
	w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, engine.Response{Code: code, Error: msg})
}

func (a *api) index(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, engine.Response{OK: true, Result: map[string]string{
		"bmdctl": a.cfg.Version, "hosts": "/hosts", "openapi": "/openapi.json"}})
}

func (a *api) hosts(w http.ResponseWriter, r *http.Request) {
	f, err := a.cfg.Hosts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, engine.CodeUsage, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, engine.Response{OK: true, Result: f})
}

// target is the device a URL's host names: a host in hosts.yml (by name,
// any unambiguous start of one, or address), else an address, an ATEM
// unless the path names the device.
type target struct {
	address, device, label string
}

func (a *api) target(spec string) (target, error) {
	f, err := a.cfg.Hosts()
	if err != nil {
		return target{}, err
	}
	h, addr, err := f.Resolve(spec)
	if err != nil {
		return target{}, err
	}
	t := target{address: addr, label: addr}
	if h != nil {
		t.device, t.label = h.Device, fmt.Sprintf("%s (%s)", h.Label(), addr)
	}
	return t, nil
}

func (a *api) state(w http.ResponseWriter, r *http.Request) {
	t, err := a.target(r.PathValue("host"))
	if err != nil {
		writeError(w, http.StatusNotFound, engine.CodeUsage, err.Error())
		return
	}
	req := engine.Request{Device: deviceOr(t.device), Host: t.address, Path: "state"}
	if p := r.URL.Query().Get("path"); p != "" {
		req.Params = map[string]string{"path": p}
	}
	a.run(w, r, req, nil)
}

func deviceOr(d string) string {
	if d == "" {
		return "atem"
	}
	return d
}

// command runs one of the table's commands: POST /hosts/{host}/{words},
// the device's word first or not.
func (a *api) command(w http.ResponseWriter, r *http.Request) {
	t, err := a.target(r.PathValue("host"))
	if err != nil {
		writeError(w, http.StatusNotFound, engine.CodeUsage, err.Error())
		return
	}
	words := strings.FieldsFunc(r.PathValue("command"), func(c rune) bool { return c == '/' })
	device := deviceOr(t.device)
	if len(words) > 0 && slices.Contains(hosts.Devices, words[0]) {
		if t.device != "" && t.device != words[0] {
			writeError(w, http.StatusBadRequest, engine.CodeUsage, fmt.Sprintf("%s is %s %s, not %s %s", t.label,
				engine.DeviceArticle(t.device), engine.DeviceNames[t.device], engine.DeviceArticle(words[0]), engine.DeviceNames[words[0]]))
			return
		}
		device, words = words[0], words[1:]
	}
	path := strings.Join(words, " ")
	e := a.cfg.Registry.Find(device, path)
	if e == nil {
		msg := fmt.Sprintf("%s has no command %q", engine.DeviceNames[device], path)
		for _, d := range a.cfg.Registry.Devices() {
			if d != device && a.cfg.Registry.Find(d, path) != nil {
				msg = fmt.Sprintf("%s is %s %s: %s is %s %s's command", t.label, engine.DeviceArticle(device),
					engine.DeviceNames[device], path, engine.DeviceArticle(d), engine.DeviceNames[d])
			}
		}
		writeError(w, http.StatusNotFound, engine.CodeUsage, msg)
		return
	}
	if e.Stream {
		writeError(w, http.StatusBadRequest, engine.CodeUsage, fmt.Sprintf("%s streams: GET /hosts/{host}/events", path))
		return
	}
	req := engine.Request{Device: device, Host: t.address, Path: e.Path, Notes: true, Ask: true}
	if req.Params, req.Files, err = args(r, e); err == nil {
		err = options(r.URL.Query(), &req)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, engine.CodeUsage, err.Error())
		return
	}
	a.run(w, r, req, e)
}

// options reads the CLI's global flags from the query: yes, timeout,
// fps.
func options(q url.Values, req *engine.Request) error {
	for k, vs := range q {
		v := vs[len(vs)-1]
		switch k {
		case "yes":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("?yes=%s: not true or false", v)
			}
			req.Yes = b
		case "timeout":
			d, err := time.ParseDuration(v)
			if f, ferr := strconv.ParseFloat(v, 64); ferr == nil {
				d, err = time.Duration(f*float64(time.Second)), nil
			}
			if err != nil || d <= 0 {
				return fmt.Errorf("?timeout=%s: not a duration like 2s", v)
			}
			req.TimeoutMS = d.Milliseconds()
		case "fps":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > 120 {
				return fmt.Errorf("?fps=%s: not a frame rate from 1 to 120", v)
			}
			req.FPS = n
		default:
			return fmt.Errorf("?%s: the query takes yes, timeout and fps; a command's arguments go in the body", k)
		}
	}
	return nil
}

// args reads a command's arguments: a JSON object (or none), or
// multipart/form-data whose file parts are the files it sends.
func args(r *http.Request, e *table.Entry) (map[string]string, map[string][]byte, error) {
	params, files := map[string]string{}, map[string][]byte{}
	known := func(name string) (*table.Param, error) {
		for i := range e.Params {
			if e.Params[i].Name == name {
				return &e.Params[i], nil
			}
		}
		return nil, fmt.Errorf("%s takes no %q", e.Path, name)
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	r.Body = http.MaxBytesReader(nil, r.Body, maxBody)
	switch ct {
	case "multipart/form-data":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, nil, fmt.Errorf("the form: %v", err)
		}
		for name, vs := range r.MultipartForm.Value {
			if _, err := known(name); err != nil {
				return nil, nil, err
			}
			params[name] = vs[len(vs)-1]
		}
		for name, fhs := range r.MultipartForm.File {
			p, err := known(name)
			if err != nil {
				return nil, nil, err
			}
			if p.Kind != table.File {
				return nil, nil, fmt.Errorf("%q is not a file %s sends", name, e.Path)
			}
			f, err := fhs[0].Open()
			if err != nil {
				return nil, nil, err
			}
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				return nil, nil, err
			}
			files[name], params[name] = b, fhs[0].Filename
		}
	case "", "application/json":
		var body map[string]json.RawMessage
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, nil, err
		}
		if len(strings.TrimSpace(string(b))) > 0 {
			if err := json.Unmarshal(b, &body); err != nil {
				return nil, nil, fmt.Errorf("the body is not a JSON object: %v", err)
			}
		}
		for name, raw := range body {
			p, err := known(name)
			if err != nil {
				return nil, nil, err
			}
			if p.Kind == table.File {
				return nil, nil, fmt.Errorf("%q is a file: send it as multipart/form-data", name)
			}
			var v any
			d := json.NewDecoder(strings.NewReader(string(raw)))
			d.UseNumber()
			if err := d.Decode(&v); err != nil {
				return nil, nil, err
			}
			if v == nil {
				continue
			}
			s, ok := table.StringValue(v)
			if !ok {
				return nil, nil, fmt.Errorf("%q: a string, a number, a bool or a list", name)
			}
			params[name] = s
		}
	default:
		return nil, nil, fmt.Errorf("the body is %s: send application/json, or multipart/form-data with files", ct)
	}
	for _, p := range e.Params {
		if p.Kind == table.File && p.Required {
			if _, ok := files[p.Name]; !ok {
				return nil, nil, fmt.Errorf("%s needs the file %q: send it as a multipart/form-data part", e.Path, p.Name)
			}
		}
	}
	return params, files, nil
}

// answer is the engine's response, with what the HTTP answer adds.
type answer struct {
	engine.Response
	Files   []engine.FileFrame
	Confirm *engine.Confirm
}

func (a answer) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(a.Response)
	if err != nil || len(a.Files) == 0 && a.Confirm == nil {
		return b, err
	}
	var m map[string]json.RawMessage
	json.Unmarshal(b, &m)
	if len(a.Files) > 0 {
		m["files"], _ = json.Marshal(a.Files)
	}
	if a.Confirm != nil {
		m["confirm"], _ = json.Marshal(a.Confirm)
	}
	// Keep the engine's own keys first, as --json prints them.
	keys := []string{"ok", "confirmed", "result", "took_ms", "code", "error", "files", "confirm"}
	var out strings.Builder
	out.WriteString("{")
	first := true
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		if !first {
			out.WriteString(",")
		}
		first = false
		kb, _ := json.Marshal(k)
		out.Write(kb)
		out.WriteString(":")
		out.Write(v)
	}
	out.WriteString("}")
	return []byte(out.String()), nil
}

// run sends a request to the engine and answers with its response.
func (a *api) run(w http.ResponseWriter, r *http.Request, req engine.Request, e *table.Entry) {
	var ans answer
	var got bool
	err := a.cfg.Call(r.Context(), req, func(f engine.Frame) error {
		switch {
		case f.File != nil:
			ans.Files = append(ans.Files, *f.File)
		case f.Note != nil && f.Note.Confirm != nil:
			ans.Confirm = f.Note.Confirm
		case f.Response != nil:
			ans.Response, got = *f.Response, true
		}
		return nil
	})
	switch {
	case err != nil:
		code := engine.CodeUnreachable
		var ee *engine.Error
		if errors.As(err, &ee) {
			code = ee.Code
		}
		writeError(w, Status(code), code, err.Error())
	case !got:
		writeError(w, http.StatusBadGateway, engine.CodeUnreachable, "the engine hung up")
	default:
		writeJSON(w, Status(ans.Code), ans)
	}
}

// events streams a device's changes as server-sent events: each of
// watch's frames, as --json prints it, one "data:" line each. The first
// is {"kind":"watching"}. ?count stops it after so many changes; an
// ATEM's ?filter and ?all are watch's.
func (a *api) events(w http.ResponseWriter, r *http.Request) {
	t, err := a.target(r.PathValue("host"))
	if err != nil {
		writeError(w, http.StatusNotFound, engine.CodeUsage, err.Error())
		return
	}
	device := deviceOr(t.device)
	e := a.cfg.Registry.Find(device, "watch")
	if e == nil {
		writeError(w, http.StatusNotFound, engine.CodeUsage, engine.DeviceNames[device]+" has no watch")
		return
	}
	req := engine.Request{Device: device, Host: t.address, Path: "watch", Params: map[string]string{}}
	for k, vs := range r.URL.Query() {
		if slices.ContainsFunc(e.Params, func(p table.Param) bool { return p.Name == k }) {
			req.Params[k] = vs[len(vs)-1]
			continue
		}
		writeError(w, http.StatusBadRequest, engine.CodeUsage, fmt.Sprintf("?%s: watch takes no such argument", k))
		return
	}
	flusher, _ := w.(http.Flusher)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	started := false
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	frames := make(chan engine.Frame)
	done := make(chan error, 1)
	go func() {
		done <- a.cfg.Call(ctx, req, func(f engine.Frame) error {
			select {
			case frames <- f:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	for {
		select {
		case f := <-frames:
			if f.Response == nil {
				continue
			}
			if !f.Response.OK && !started {
				writeJSON(w, Status(f.Response.Code), f.Response)
				return
			}
			if !started {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.WriteHeader(http.StatusOK)
				started = true
			}
			fmt.Fprintf(w, "data: %s\n\n", f.Line)
			if flusher != nil {
				flusher.Flush()
			}
		case <-keepalive.C:
			if started {
				fmt.Fprint(w, ": keepalive\n\n")
				if flusher != nil {
					flusher.Flush()
				}
			}
		case err := <-done:
			if !started {
				if err == nil {
					err = errors.New("the engine hung up")
				}
				writeError(w, http.StatusBadGateway, engine.CodeUnreachable, err.Error())
			}
			return
		case <-r.Context().Done():
			return
		}
	}
}
