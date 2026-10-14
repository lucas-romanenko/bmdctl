// SPDX-License-Identifier: MIT

package fakehyperdeck

import (
	"bufio"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestParams(t *testing.T) {
	for in, want := range map[string]paramSet{
		"slot id: 2": {"slot id": "2"},
		"clip id: 3 loop: true single clip: true": {"clip id": "3", "loop": "true", "single clip": "true"},
		"name: Intro Loop animation.mp4":          {"name": "Intro Loop animation.mp4"},
		"clip id: 3 name: a b.mov":                {"clip id": "3", "name": "a b.mov"},
		"transport: true slot: false":             {"transport": "true", "slot": "false"},
		"timecode: 00:00:10:00":                   {"timecode": "00:00:10:00"},
		"clip id: +1":                             {"clip id": "+1"},
		"odd key: 5":                              {"odd key": "5"},
	} {
		if got := params(in); !reflect.DeepEqual(got, want) {
			t.Errorf("params(%q) = %q, want %q", in, got, want)
		}
	}
}

// talk sends lines to a fresh deck and returns everything it wrote.
func talk(t *testing.T, d *Deck, lines ...string) string {
	t.Helper()
	c, _ := d.Dial(context.Background(), "tcp", Address)
	defer c.Close()
	br := bufio.NewReader(c)
	var out strings.Builder
	read := func() {
		for {
			l, err := br.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			out.WriteString(l)
			head := strings.TrimRight(l, "\r\n")
			if !strings.HasSuffix(head, ":") {
				return
			}
			for l != "\r\n" {
				l, _ = br.ReadString('\n')
				out.WriteString(l)
			}
			return
		}
	}
	read() // the greeting
	for _, l := range lines {
		c.Write([]byte(l + "\n"))
		read()
	}
	return out.String()
}

func TestAnswers(t *testing.T) {
	d := New()
	got := talk(t, d, "ping", "frob", "goto: clip id: 9", "clips add: name: nope.mov", "play: speed: 9000",
		"goto: timecode: 99:00:00:00", "slot select: slot id: 3")
	for _, want := range []string{"500 connection info:\r\nprotocol version: 1.13\r\nmodel: HyperDeck Studio HD Mini\r\n\r\n",
		"200 ok\r\n100 syntax error\r\n112 clip not found\r\n112 clip not found\r\n102 invalid value\r\n109 out of range\r\n102 invalid value\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
	d.SetRemote(false)
	if got := talk(t, d, "stop"); !strings.HasSuffix(got, "111 remote control disabled\r\n") {
		t.Errorf("%q", got)
	}
}

func TestRecordAddsAClip(t *testing.T) {
	d := New()
	talk(t, d, "record", "stop", "record: name: Take 2", "stop")
	clips := d.Slot(1)
	if len(clips) != 5 || clips[3].Name != "Clip 0001.mov" || clips[4].Name != "Take 2.mov" {
		t.Errorf("%+v", clips)
	}
	if got := talk(t, d, "clips count"); !strings.Contains(got, "clip count: 5") {
		t.Errorf("%q", got)
	}
}
