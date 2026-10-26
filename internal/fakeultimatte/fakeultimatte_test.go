// SPDX-License-Identifier: MIT

package fakeultimatte

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/lucas-romanenko/bmdctl/ultimatte"
)

var ctx = context.Background()

// client is an ultimatte.Client on u with short quiets: a reply whose
// last field has a value is read to the quiet, as on a unit.
func client(u *Unit) *ultimatte.Client {
	return &ultimatte.Client{DialContext: u.Dial, PreludeQuiet: 50 * time.Millisecond, ReplyQuiet: 50 * time.Millisecond,
		PreludeDrain: 10 * time.Millisecond, ReadbackPoll: 10 * time.Millisecond}
}

// Over loopback, both channels on real TCP connections: Listen serves
// what Dial's pipes do. The client dials 9998 and 9996; this dialer
// takes each to its listener.
func TestOverLoopback(t *testing.T) {
	u := New()
	lc, ls, err := u.Listen("127.0.0.1:0", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lc.Close()
	defer ls.Close()
	to := map[string]string{strconv.Itoa(ultimatte.ControlPort): lc.Addr().String(), strconv.Itoa(ultimatte.SettingsPort): ls.Addr().String()}
	c := client(u)
	c.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, _ := net.SplitHostPort(addr)
		var d net.Dialer
		return d.DialContext(ctx, network, to[port])
	}
	if _, info, err := c.Archive(ctx, Address, time.Now(), nil); err != nil || info.Label != "Keyer_A" || len(info.Slots) != 3 {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestArchiveAndRestore(t *testing.T) {
	u := New()
	c := client(u)
	z, info, err := c.Archive(ctx, Address, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Label != "Keyer_A" || !slices.Equal(info.Slots, []string{"Green Screen", "Blue Screen", "Studio B"}) ||
		len(info.Warnings) != 0 || info.SavedSize != 300 {
		t.Fatalf("%+v", info)
	}
	r, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil || len(r.File) != 9 || r.File[5].Name != "presets/Green Screen" {
		t.Fatalf("%v %d", err, len(r.File))
	}
	ok, rinfo, err := c.Restore(ctx, Address, z, nil)
	if err != nil || !ok || len(rinfo.Presets) != 3 || rinfo.Saved == nil || !rinfo.Saved.OK {
		t.Fatalf("%v %+v %v", ok, rinfo, err)
	}
	w := u.Writes()
	if len(w) != 5 || w[0].Name != ultimatte.WireNamePrefix+"/presets/Blue Screen" || w[4].Opcode != ultimatte.OpWriteResource {
		t.Fatalf("%+v", w)
	}
}

func TestNetwork(t *testing.T) {
	u := New()
	c := client(u)
	iface, err := c.ReadNetwork(ctx, Address, 0)
	if err != nil || iface.Address != Address || iface.StaticNetmask != "255.255.252.0" || iface.Dynamic == nil || *iface.Dynamic {
		t.Fatalf("%+v %v", iface, err)
	}
	addr, mask := "192.0.2.42", "255.255.248.0"
	iface, err = c.SetNetwork(ctx, Address, 0, ultimatte.NetworkSet{Address: &addr, Netmask: &mask})
	if err != nil || iface.Address != addr || iface.Netmask != mask {
		t.Fatalf("%+v %v", iface, err)
	}
	if a, m := u.Static(); a != addr || m != mask {
		t.Errorf("static %s/%s", a, m)
	}
	// Four blocks: the first read, the set, the read-back that saw
	// 0.0.0.0, the one that settled.
	if b := u.Blocks(); len(b) != 4 || b[1] != "NETWORK INTERFACE 0:\nStatic Addresses: 192.0.2.42/255.255.248.0" {
		t.Errorf("%q", b)
	}
	bad := "1.2.3.4/24"
	_, err = c.SetNetwork(ctx, Address, 0, ultimatte.NetworkSet{Address: &bad, Netmask: new("")})
	if !errors.Is(err, ultimatte.ErrRefused) {
		t.Errorf("a prefix length: %v", err)
	}
	u.Down(true)
	if _, err := c.ReadNetwork(ctx, Address, 0); err == nil {
		t.Error("down, but answered")
	}
}
