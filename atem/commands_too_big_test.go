// Copyright 2021 - 2022, Martijn Braam and the OpenAtem contributors
// Copyright 2026, Lucas Romanenko
// SPDX-License-Identifier: LGPL-3.0-only

package atem

// Command packets too big for the path go again split (bmdwire#102,
// 3a65748; test_commands_too_big.py, one for one). A packet of commands
// can carry up to 1,300 bytes of them (20 labels, a profile restore).
// Over a path that drops datagrams that big it never got through, and the
// switcher, which takes our packets in order only, took nothing after it
// either: 20 labels at MTU 576 never arrived, and a profile restore over
// a VPN at MTU 1,200 sat out its macro upload's timeout with nothing
// restored.

import (
	"bytes"
	"slices"
	"testing"
	"time"
)

// labels is one packet's worth of n label commands, 40 bytes each, as
// sendCommands packs them.
func labels(n int) []byte {
	var b []byte
	for i := 1; i <= n; i++ {
		b = append(b, cmdBytes("CInL", bytes.Repeat([]byte{byte(i)}, 32))...)
	}
	return b
}

// stuckCommandPacket is a packet the switcher acknowledged, then pkt,
// which it never gets.
func stuckCommandPacket(t *testing.T, pkt []byte) (*transport, *fakeConn, *time.Time) {
	t.Helper()
	tr, c := established(t)
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }
	sendReliable(t, tr, []byte("\x00\x08TEST"))
	now = now.Add(80 * time.Millisecond)
	tr.onACK(1, true)
	sendReliable(t, tr, pkt)
	return tr, c, &now
}

func reliableSent(c *fakeConn) []packet {
	var out []packet
	for _, p := range c.sent() {
		if p.flags&flagReliable != 0 {
			out = append(out, p)
		}
	}
	return out
}

func TestAStuckPacketOfCommandsGoesAgainSplit(t *testing.T) { // test_a_stuck_packet_of_commands_goes_again_split
	clearChunkLimits(t)
	pkt := labels(30) // 1,200 bytes of commands
	tr, c, now := stuckCommandPacket(t, pkt)
	c.reset()
	if tb := timerFires(tr, now, tooBigAfter+1, true); tb != nil {
		t.Fatalf("taken back %+v: no upload among them", tb)
	}
	var again []packet
	for _, p := range reliableSent(c) {
		if p.seq >= 2 && p.flags&flagRetransmission == 0 {
			again = append(again, p)
		}
	}
	if seqs := seqsOf(again); !slices.Equal(seqs, []uint16{2, 3}) {
		t.Fatalf("sent again %v: not from where it stood, in order", seqs)
	}
	var got []byte
	for _, p := range again {
		if len(p.data) > 952+commandRoom {
			t.Fatalf("a packet of %d bytes", len(p.data))
		}
		got = append(got, p.data...)
	}
	if !bytes.Equal(got, pkt) {
		t.Fatal("not every command, in order")
	}
	if tr.maxChunk != 952 || chunkLimit(switcherAddr) != 952 {
		t.Fatalf("maxChunk %d, kept %d", tr.maxChunk, chunkLimit(switcherAddr))
	}
}

func TestOnceTheSizeIsKnownCommandPacketsAreSplitAsTheyGo(t *testing.T) { // test_once_the_size_is_known_command_packets_are_split_as_they_go
	tr, c := established(t)
	tr.maxChunk = 952
	pkt := labels(30)
	sendReliable(t, tr, pkt)
	out := reliableSent(c)
	var got []byte
	for _, p := range out {
		if len(p.data) > 964 {
			t.Fatalf("a packet of %d bytes", len(p.data))
		}
		got = append(got, p.data...)
	}
	if len(out) != 2 || !bytes.Equal(got, pkt) {
		t.Fatalf("%d packets", len(out))
	}
}

// Nothing to split it into: resent as it is, the size unchanged.
func TestAPacketOfOneCommandIsLeftAlone(t *testing.T) { // test_a_packet_of_one_command_is_left_alone
	clearChunkLimits(t)
	tr, _, now := stuckCommandPacket(t, cmdBytes("CBIG", make([]byte, 1272)))
	timerFires(tr, now, tooBigAfter+2, true)
	if tr.maxChunk != 0 {
		t.Fatalf("maxChunk %d", tr.maxChunk)
	}
}

func TestAQuietLinkIsNotASmallPath(t *testing.T) { // test_a_quiet_link_is_not_a_small_path
	clearChunkLimits(t)
	tr, _, now := stuckCommandPacket(t, labels(30))
	timerFires(tr, now, tooBigAfter+2, false)
	if tr.maxChunk != 0 {
		t.Fatalf("maxChunk %d", tr.maxChunk)
	}
}

func TestChunksBehindAStuckCommandPacketGoBackToTheirUpload(t *testing.T) { // test_chunks_behind_a_stuck_command_packet_go_back_to_their_upload
	clearChunkLimits(t)
	tr, _, now := stuckCommandPacket(t, labels(30))
	for n := range 3 {
		sendReliable(t, tr, ftda(byte(n), 1392))
	}
	tb := timerFires(tr, now, tooBigAfter+1, true)
	if tb == nil || tb.transfer != 7 || !slices.Equal(tb.sizes, []int{1392, 1392, 1392}) || tb.limit != 952 {
		t.Fatalf("taken back %+v", tb)
	}
}

func TestCommandsSplitOnlyBetweenCommands(t *testing.T) { // test_commands_split_only_between_commands
	pkt := labels(5)
	if cmds := commandsOf(pkt); len(cmds) != 5 {
		t.Fatalf("%d commands", len(cmds))
	}
	var sizes []int
	for _, q := range splitCommands(&packet{flags: flagReliable, data: pkt}, 100) {
		sizes = append(sizes, len(q.data))
	}
	if !slices.Equal(sizes, []int{80, 80, 40}) {
		t.Fatalf("sizes %v", sizes)
	}
	if q := splitCommands(&packet{flags: flagReliable, data: []byte("\x00\x08TEST")}, 4); len(q) != 1 || string(q[0].data) != "\x00\x08TEST" {
		t.Fatalf("split %v", q)
	}
	if commandsOf([]byte("\x00\x09TEST")) != nil {
		t.Fatal("a length running past the end")
	}
}

// A stuck packet of commands with a small packet after it goes again
// split in this session (bmdwire 700a576). bmdwire#100 (e4119f7) starts a
// new session when a smaller packet follows a stuck one, so that no
// sequence number the switcher may hold goes again with other data; an
// upload's chunks are carried over to it, a command packet's commands
// are not, and a profile restore over a path MTU of 1,200 or 576 lost its
// labels.
func TestAStuckPacketOfCommandsWithASmallOneAfterItGoesAgainInThisSession(t *testing.T) {
	clearChunkLimits(t)
	pkt := labels(30) // 1,200 bytes of commands
	tr, c, now := stuckCommandPacket(t, pkt)
	sendReliable(t, tr, []byte("\x00\x08TEST")) // small, after it
	c.reset()
	if tb := timerFires(tr, now, tooBigAfter+1, true); tb != nil {
		t.Fatalf("taken back %+v: no upload among them", tb)
	}
	if tr.newSessionAsked != "" {
		t.Fatalf("a new session asked for (%s): its commands would be lost", tr.newSessionAsked)
	}
	var again []byte
	for _, p := range reliableSent(c) {
		if p.seq >= 2 && p.flags&flagRetransmission == 0 {
			again = append(again, p.data...)
		}
	}
	if !bytes.Equal(again, append(slices.Clone(pkt), "\x00\x08TEST"...)) {
		t.Fatal("not every command, then the small packet, in order")
	}
	if tr.maxChunk != 952 {
		t.Fatalf("maxChunk %d", tr.maxChunk)
	}
}
