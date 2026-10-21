// SPDX-License-Identifier: MIT

package ultimatte

// The 9998 block protocol and the network interface, bmdwire v1.6.0's
// ultimattewire/network.py: build_block, parse_interface, exchange,
// read_network, set_network.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lucas-romanenko/bmdctl/internal/pytext"
)

// NetworkInterface is one NETWORK INTERFACE n section as the unit reports
// it. Address and Netmask are the current ones (what it uses); the Static
// ones what it is configured with: they differ on DHCP and while the
// interface reapplies. Priority and Dynamic are nil where the unit
// doesn't say (or says something else).
type NetworkInterface struct {
	Index         int      `json:"index"`
	Name          string   `json:"name"`
	MAC           string   `json:"mac"`
	Priority      *int     `json:"priority"`
	Dynamic       *bool    `json:"dynamic"`
	Address       string   `json:"address"`
	Netmask       string   `json:"netmask"`
	Gateway       string   `json:"gateway"`
	DNS           []string `json:"dns"`
	StaticAddress string   `json:"static_address"`
	StaticNetmask string   `json:"static_netmask"`
	StaticGateway string   `json:"static_gateway"`
	StaticDNS     []string `json:"static_dns"`
	Raw           string   `json:"raw"`
}

// SectionName is section_name: "NETWORK INTERFACE <index>".
func SectionName(index int) string { return "NETWORK INTERFACE " + strconv.Itoa(index) }

// Field is one "key: value" line of a block.
type Field struct{ Key, Value string }

// BuildBlock is build_block: the section's header, the fields in their
// order, and the blank line that submits the block. No fields is the
// query form.
func BuildBlock(index int, fields ...Field) string {
	lines := []string{SectionName(index) + ":"}
	for _, f := range fields {
		lines = append(lines, f.Key+": "+f.Value)
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// splitAddress is _split_address: the first of a comma- or
// space-separated list, split at "/" into the address and the dotted
// mask, each stripped; no "/" gives an empty mask.
func splitAddress(v string) (string, string) {
	first := pytext.Fields(strings.ReplaceAll(v, ",", " "))
	if len(first) == 0 {
		return "", ""
	}
	ip, mask, ok := strings.Cut(first[0], "/")
	if !ok {
		return pytext.Strip(first[0]), ""
	}
	return pytext.Strip(ip), pytext.Strip(mask)
}

// splitList is _split_list.
func splitList(v string) []string {
	out := pytext.Fields(strings.ReplaceAll(v, ",", " "))
	if out == nil {
		return []string{}
	}
	return out
}

// ParseInterface is parse_interface: each field the first line that
// starts "<key>:", its value stripped, "" where none does. A Priority
// that is digits but not decimal ones (a superscript) is int()'s
// ValueError, as there.
func ParseInterface(text string, index int) (NetworkInterface, error) {
	lines := strings.Split(text, "\n")
	get := func(key string) string {
		for _, l := range lines {
			if rest, ok := strings.CutPrefix(l, key+":"); ok {
				return pytext.Strip(rest)
			}
		}
		return ""
	}
	iface := NetworkInterface{Index: index, Raw: text, Name: get("Name"), MAC: get("MAC Address")}
	if p := get("Priority"); pytext.IsDigit(p) {
		n, err := pytext.Int(p)
		if err != nil {
			return NetworkInterface{}, &Error{Class: "ValueError", Msg: err.Error(), kind: ErrValue}
		}
		v := int(n)
		iface.Priority = &v
	}
	switch pytext.Lower(get("Dynamic IP")) {
	case "true":
		iface.Dynamic = new(true)
	case "false":
		iface.Dynamic = new(false)
	}
	iface.Address, iface.Netmask = splitAddress(get("Current Addresses"))
	iface.Gateway = get("Current Gateway")
	iface.DNS = splitList(get("Current DNS Servers"))
	iface.StaticAddress, iface.StaticNetmask = splitAddress(get("Static Addresses"))
	iface.StaticGateway = get("Static Gateway")
	iface.StaticDNS = splitList(get("Static DNS Servers"))
	return iface, nil
}

func networkError(format string, args ...any) *Error {
	return &Error{Class: "UltimatteNetworkError", Msg: fmt.Sprintf(format, args...)}
}

// Exchange is exchange: one block on a fresh 9998 session. It reads the
// prelude (to END PRELUDE, PreludeQuiet per read, nothing parsed), sends
// the block, reads the reply (ReplyQuiet per read, until a read times
// out, the unit closes, or the reply is a NAK, or an ACK with two blank
// lines whose last character that isn't whitespace is a colon), and
// returns what follows the ACK. A NAK, or a reply that isn't an ACK, is
// an *Error.
func (c *Client) Exchange(ctx context.Context, host, block string) (string, error) {
	_, body, err := c.exchange(ctx, host, block)
	return body, err
}

// exchange is Exchange, with the prelude it read: what a unit's label
// is read from where nothing else is (bmdctl's addition).
func (c *Client) exchange(ctx context.Context, host, block string) (prelude, body string, err error) {
	conn, err := c.connect(ctx, host, ControlPort, c.connectTimeout())
	if err != nil {
		return "", "", err
	}
	stop := watch(ctx, conn)
	pre, err := c.prelude(ctx, conn, false)
	var reply string
	if err == nil {
		if err = write(ctx, conn, []byte(block), or(c.ReplyQuiet, DefaultReplyQuiet)); err == nil {
			reply, err = c.reply(ctx, conn)
		}
	}
	stop()
	conn.Close()
	prelude = pytext.Decode(pre)
	if err != nil {
		return prelude, "", err
	}
	stripped := strings.TrimLeftFunc(reply, pytext.IsSpace)
	switch {
	case strings.HasPrefix(stripped, "NAK"):
		e := networkError("unit refused the block (NAK):\n%s", pytext.Strip(block))
		e.kind = ErrRefused
		return prelude, "", e
	case !strings.HasPrefix(stripped, "ACK"):
		head := []rune(reply)
		e := networkError("no ACK from %s (got %s)", host, pytext.Repr(string(head[:min(80, len(head))])))
		e.kind = ErrShort
		return prelude, "", e
	}
	return prelude, strings.TrimLeft(stripped[len("ACK"):], "\n"), nil
}

// reply is _read_reply.
func (c *Client) reply(ctx context.Context, conn interface {
	Read([]byte) (int, error)
	SetReadDeadline(time.Time) error
}) (string, error) {
	quiet := or(c.ReplyQuiet, DefaultReplyQuiet)
	var buf []byte
	chunk := make([]byte, 65536)
	for {
		conn.SetReadDeadline(deadline(ctx, quiet))
		n, err := conn.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if n == 0 {
			switch {
			case timedOut(err) && ctx.Err() == nil, errors.Is(err, io.EOF):
				return pytext.Decode(buf), nil
			case err != nil:
				return pytext.Decode(buf), contextOr(ctx, err)
			}
			continue
		}
		text := pytext.Decode(buf)
		if strings.HasPrefix(text, "NAK") {
			break
		}
		if strings.HasPrefix(text, "ACK") && strings.HasSuffix(strings.TrimRightFunc(text, pytext.IsSpace), ":") &&
			strings.Count(text, "\n\n") >= 2 {
			break
		}
	}
	return pytext.Decode(buf), nil
}

// ReadNetwork is read_network: the query form of NETWORK INTERFACE
// <index>, parsed. It sets nothing.
func (c *Client) ReadNetwork(ctx context.Context, host string, index int) (NetworkInterface, error) {
	iface, _, err := c.readNetwork(ctx, host, index)
	return iface, err
}

// readNetwork is ReadNetwork, with the prelude the exchange read.
func (c *Client) readNetwork(ctx context.Context, host string, index int) (NetworkInterface, string, error) {
	pre, body, err := c.exchange(ctx, host, BuildBlock(index))
	if err != nil {
		return NetworkInterface{}, pre, err
	}
	iface, err := ParseInterface(body, index)
	return iface, pre, err
}

// NetworkSet is what SetNetwork sets: nil leaves a field alone.
// Address and Netmask are one field on the wire and go together. DNS
// set to an empty list clears it.
type NetworkSet struct {
	Address, Netmask, Gateway *string
	DNS                       *[]string
	Dynamic                   *bool
}

// fields are the block's fields, in set_network's order.
func (s NetworkSet) fields() ([]Field, error) {
	if (s.Address == nil) != (s.Netmask == nil) {
		return nil, &Error{Class: "ValueError", Msg: "address and netmask are one field on the wire — pass both or neither", kind: ErrValue}
	}
	var fs []Field
	if s.Dynamic != nil {
		fs = append(fs, Field{"Dynamic IP", strconv.FormatBool(*s.Dynamic)})
	}
	if s.Address != nil {
		fs = append(fs, Field{"Static Addresses", *s.Address + "/" + *s.Netmask})
	}
	if s.Gateway != nil {
		fs = append(fs, Field{"Static Gateway", *s.Gateway})
	}
	if s.DNS != nil {
		fs = append(fs, Field{"Static DNS Servers", strings.Join(*s.DNS, " ")})
	}
	if len(fs) == 0 {
		return nil, &Error{Class: "ValueError", Msg: "nothing to set", kind: ErrValue}
	}
	return fs, nil
}

// SetNetwork is set_network: it writes the static settings and returns
// the interface read back once it has settled, never the set's own echo
// (which carries only the fields sent). An address change makes the
// interface reapply, briefly reporting 0.0.0.0, so the read-back is
// repeated every ReadbackPoll until ReadbackTimeout: settled is the
// current address present, not 0.0.0.0, and equal (with its mask) to the
// static one; a set that touches no address doesn't wait. Never settling
// is an *Error naming the unit: it accepted the change and may be out of
// reach. It writes what it is told: whether a change is safe is the
// caller's to decide.
func (c *Client) SetNetwork(ctx context.Context, host string, index int, set NetworkSet) (NetworkInterface, error) {
	fields, err := set.fields()
	if err != nil {
		return NetworkInterface{}, err
	}
	if _, err := c.Exchange(ctx, host, BuildBlock(index, fields...)); err != nil {
		return NetworkInterface{}, err
	}
	now, sleep := c.now, c.sleep
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	timeout := or(c.ReadbackTimeout, DefaultReadbackTimeout)
	if c.ReadbackTimeout < 0 {
		timeout = 0
	}
	end := now().Add(timeout)
	var last string
	for {
		iface, err := c.ReadNetwork(ctx, host, index)
		switch {
		case err == nil && settled(iface, set.Address != nil):
			return iface, nil
		case err == nil:
			last = fmt.Sprintf("interface still reapplying (Current %s/%s)", iface.Address, iface.Netmask)
		case isValueError(err) || ctx.Err() != nil:
			return NetworkInterface{}, err // parse_interface's ValueError isn't caught
		default:
			last = err.Error()
		}
		if !now().Before(end) {
			break
		}
		if err := sleep(ctx, or(c.ReadbackPoll, DefaultReadbackPoll)); err != nil {
			return NetworkInterface{}, err
		}
	}
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	slices.Sort(keys) // sorted(fields), then the list's repr
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = pytext.Repr(k)
	}
	e := networkError("%s: the unit accepted [%s] but did not settle within %ss — CHECK THIS UNIT (%s)",
		host, strings.Join(names, ", "), strconv.FormatFloat(timeout.Seconds(), 'g', 6, 64), last)
	e.kind = ErrUnsettled
	return NetworkInterface{}, e
}

func isValueError(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Class == "ValueError"
}

// unconfigured is what Current Addresses reads mid-reapply.
const unconfigured = "0.0.0.0"

// settled is _settled: has the interface finished reapplying?
func settled(iface NetworkInterface, addressWasSet bool) bool {
	if !addressWasSet {
		return true
	}
	if iface.Address == "" || iface.Address == unconfigured {
		return false
	}
	return iface.Address == iface.StaticAddress && iface.Netmask == iface.StaticNetmask
}
