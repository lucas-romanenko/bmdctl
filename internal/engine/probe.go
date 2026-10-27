// SPDX-License-Identifier: MIT

package engine

// The device probe (docs/PORTING.md, "Device probe"): what answers at an
// address, asked four ways at once, for connect ADDRESS and hosts add.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/atem"
	"github.com/lucas-romanenko/bmdctl/hyperdeck"
	"github.com/lucas-romanenko/bmdctl/ultimatte"
)

// ProbeTimeout caps a probe: whatever hasn't answered by then hasn't.
const ProbeTimeout = 2 * time.Second

// Each device's port, where a probe asks it.
const (
	PortATEM      = atem.DefaultPort      // UDP
	PortHyperDeck = hyperdeck.DefaultPort // TCP
	PortVideohub  = 9990                  // TCP
	PortUltimatte = 9998                  // TCP
)

// ProbeResult is the device that answered at an address.
type ProbeResult struct {
	Address string `json:"address"`
	Device  string `json:"device"` // atem, hyperdeck, videohub, ultimatte
	// Name is what the device calls itself, where the probe can ask:
	// a HyperDeck's device info, a Videohub's friendly name, an
	// Ultimatte's label. An ATEM's needs its state: the session that
	// follows has it.
	Name  string `json:"name,omitempty"`
	Model string `json:"model,omitempty"`
}

// DeviceNames are the devices as a person reads them.
var DeviceNames = map[string]string{"atem": "ATEM", "hyperdeck": "HyperDeck", "videohub": "Videohub", "ultimatte": "Ultimatte"}

// DeviceArticle is "a" or "an", for "<name> is a HyperDeck".
func DeviceArticle(device string) string {
	switch device {
	case "atem", "ultimatte":
		return "an"
	}
	return "a"
}

// probe asks addr, at once, whether it is an ATEM (the UDP handshake's
// first reply), a HyperDeck (TCP 9993's 500 connection info), a Videohub
// (TCP 9990's PROTOCOL PREAMBLE and VIDEOHUB DEVICE) or an Ultimatte (TCP
// 9998's prelude). The first to answer within ProbeTimeout wins. An
// address with a port is asked each way on that port.
func (e *Engine) probe(ctx context.Context, addr string) (ProbeResult, error) {
	host, port := addr, ""
	if h, p, err := net.SplitHostPort(addr); err == nil {
		host, port = h, p
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return ProbeResult{}, codeErr(CodeUsage, "no host in %q", addr)
	}
	at := func(def int) string {
		if port != "" {
			return net.JoinHostPort(host, port)
		}
		return net.JoinHostPort(host, strconv.Itoa(def))
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	type leg struct {
		r   ProbeResult
		err error
	}
	legs := []func() (ProbeResult, error){
		func() (ProbeResult, error) {
			return ProbeResult{Device: "atem"}, e.cfg.ProbeATEM(ctx, at(PortATEM))
		},
		func() (ProbeResult, error) { return e.probeHyperDeck(ctx, at(PortHyperDeck)) },
		func() (ProbeResult, error) { return e.probeVideohub(ctx, at(PortVideohub), port == "") },
		func() (ProbeResult, error) { return e.probeUltimatte(ctx, at(PortUltimatte), port == "") },
	}
	out := make(chan leg, len(legs))
	for _, f := range legs {
		go func() { r, err := f(); out <- leg{r, err} }()
	}
	var errs []string
	for range legs {
		l := <-out
		if l.err == nil {
			l.r.Address = addr
			e.log.Info("probe", "host", addr, "device", l.r.Device, "name", l.r.Name)
			return l.r, nil
		}
		errs = append(errs, l.err.Error())
	}
	e.log.Info("probe: nothing answered", "host", addr, "errs", strings.Join(errs, "; "))
	if errors.Is(ctx.Err(), context.Canceled) { // the caller hung up
		return ProbeResult{}, codeErr(CodeUnreachable, "probe of %s cancelled", addr)
	}
	return ProbeResult{}, codeErr(CodeUnreachable, "nothing answered at %s within %v as an ATEM (UDP %d), a HyperDeck (TCP %d), "+
		"a Videohub (TCP %d) or an Ultimatte (TCP %d)", addr, ProbeTimeout, PortATEM, PortHyperDeck, PortVideohub, PortUltimatte)
}

// probeHyperDeck reads the greeting and, for the name, device info.
func (e *Engine) probeHyperDeck(ctx context.Context, addr string) (ProbeResult, error) {
	d := hyperdeck.Dialer{Bare: true, DialContext: e.cfg.ProbeDial, Logger: e.log}
	s, err := d.Dial(ctx, addr)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("hyperdeck: %w", err)
	}
	defer s.Close()
	if g := s.Greeting(); g.Code != 500 || !strings.EqualFold(g.Text, "connection info") {
		return ProbeResult{}, fmt.Errorf("hyperdeck: %s greeted %d %s", addr, g.Code, g.Text)
	}
	r := ProbeResult{Device: "hyperdeck", Model: s.Model()}
	if info, err := s.DeviceInfo(ctx); err == nil {
		st := hyperdeck.State{Device: info, Connection: map[string]string{"model": r.Model}}
		r.Name, r.Model = st.Name(), st.Model()
	}
	if r.Name == "" {
		r.Name = r.Model
	}
	return r, nil
}

// preamble reads the text a Videohub or Ultimatte sends on connect, up to
// a line until reports done, the context's end, or the connection's.
func (e *Engine) preamble(ctx context.Context, addr string, until func(line string) bool) ([]string, error) {
	c, err := e.cfg.ProbeDial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if dl, ok := ctx.Deadline(); ok {
		c.SetReadDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	var lines []string
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		l := strings.TrimRight(sc.Text(), "\r")
		lines = append(lines, l)
		if until(l) {
			break
		}
	}
	return lines, nil
}

// keyed reads "Key: value" lines, keys lower-cased.
func keyed(lines []string) map[string]string {
	m := map[string]string{}
	for _, l := range lines {
		if k, v, ok := strings.Cut(l, ":"); ok && v != "" {
			m[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return m
}

// probeVideohub reads the preamble to the end of its VIDEOHUB DEVICE
// block (videohubwire's _read_preamble): PROTOCOL PREAMBLE first, and the
// device's friendly and model names in it. On another port than 9990 the
// device block must be there, since an Ultimatte's prelude starts the
// same way.
func (e *Engine) probeVideohub(ctx context.Context, addr string, ownPort bool) (ProbeResult, error) {
	inDevice, devSeen := false, false
	lines, err := e.preamble(ctx, addr, func(l string) bool {
		switch {
		case strings.TrimSpace(l) == "VIDEOHUB DEVICE:":
			inDevice, devSeen = true, true
		case inDevice && strings.TrimSpace(l) == "":
			return true
		case strings.TrimSpace(l) == "END PRELUDE:":
			return true
		}
		return false
	})
	if err != nil && len(lines) == 0 {
		return ProbeResult{}, fmt.Errorf("videohub: %w", err)
	}
	first := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = strings.TrimSpace(l)
			break
		}
	}
	if first != "PROTOCOL PREAMBLE:" || !ownPort && !devSeen {
		return ProbeResult{}, fmt.Errorf("videohub: %s sent no preamble", addr)
	}
	kv := keyed(lines)
	r := ProbeResult{Device: "videohub", Name: kv["friendly name"], Model: kv["model name"]}
	if r.Name == "" {
		r.Name = r.Model
	}
	return r, nil
}

// probeUltimatte reads the prelude on 9998 to END PRELUDE: (ultimattewire's
// read_preamble); its Label is the unit's name. A Videohub's device block
// in it means it isn't one.
func (e *Engine) probeUltimatte(ctx context.Context, addr string, ownPort bool) (ProbeResult, error) {
	lines, err := e.preamble(ctx, addr, func(l string) bool { return strings.TrimSpace(l) == "END PRELUDE:" })
	if len(lines) == 0 {
		if err == nil {
			err = errors.New("nothing sent")
		}
		return ProbeResult{}, fmt.Errorf("ultimatte: %w", err)
	}
	text := strings.Join(lines, "\n")
	kv := keyed(lines)
	_, label := kv["label"]
	ok := strings.Contains(text, "PROTOCOL PREAMBLE:") || label || strings.Contains(text, "END PRELUDE:")
	if !ok || strings.Contains(text, "VIDEOHUB DEVICE:") || !ownPort && !label {
		return ProbeResult{}, fmt.Errorf("ultimatte: %s sent no prelude", addr)
	}
	model := ultimatte.ExtractModel(text) // IDENTITY's, bmdwire#52
	if model == "" {
		model = "Ultimatte"
	}
	return ProbeResult{Device: "ultimatte", Name: kv["label"], Model: model}, nil
}
