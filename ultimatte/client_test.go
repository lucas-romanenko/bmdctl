// SPDX-License-Identifier: MIT

package ultimatte

// bmdwire v1.6.0's ultimattewire/tests (test_preamble.py,
// test_protocol.py, test_profile.py, test_network.py), ported one for
// one: each test names its Python counterpart. The Python tests fake the
// sockets, or the module seams (read_preamble, binary_request,
// upload_one); here every connection is scripted (conn_test.go), so a
// seam's fake is the connections it would open.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

const host = "192.0.2.21"

var ctx = context.Background()

// ---- read_preamble (test_preamble.py) --------------------------------------

// test_read_preamble_reads_until_end_prelude_then_drains: the quiet
// timeout first, then the short drain after END PRELUDE.
func TestReadPreambleReadsUntilEndPreludeThenDrains(t *testing.T) {
	pre := fixture(t, "sample-prelude.txt")
	s := newScript(pre[:20], pre[20:], []byte("trailing\n"), timeout{})
	c, _ := client(s)
	text, err := c.ReadPrelude(ctx, host)
	if err != nil || text != string(pre)+"trailing\n" || !s.isClosed() {
		t.Fatalf("%q %v", text, err)
	}
	if s.timeouts[0] != DefaultPreludeQuiet || s.timeouts[len(s.timeouts)-1] != DefaultPreludeDrain {
		t.Errorf("timeouts %v", s.timeouts)
	}
}

// test_read_preamble_returns_partial_text_on_early_close: the unit may
// drop the connection mid-prelude; what arrived is returned.
func TestReadPreambleReturnsPartialTextOnEarlyClose(t *testing.T) {
	s := newScript([]byte("Label: Keyer A\nCONT"))
	c, _ := client(s)
	if text, err := c.ReadPrelude(ctx, host); err != nil || text != "Label: Keyer A\nCONT" || !s.isClosed() {
		t.Fatalf("%q %v", text, err)
	}
}

// ---- parse_section / parse_file_list / extract_label -----------------------

// test_parse_section_returns_body_up_to_next_header
func TestParseSectionReturnsBodyUpToNextHeader(t *testing.T) {
	got := strings.Split(ParseSection(string(fixture(t, "sample-prelude.txt")), "CONTROL"), "\n")
	want := []string{"Matte Density: 5000", "Transition Rate: 30", "Offset Black Balance: 2500", "Not A Param: 12"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

// test_parse_section_multi_line_file_list_is_not_truncated
func TestParseSectionMultiLineFileListIsNotTruncated(t *testing.T) {
	if got := ParseSection(string(fixture(t, "sample-prelude.txt")), "FILE LIST"); got != "preset-a\npreset-b" {
		t.Fatalf("%q", got)
	}
}

// test_parse_section_empty_section_does_not_swallow_the_next
func TestParseSectionEmptySectionDoesNotSwallowTheNext(t *testing.T) {
	if got := ParseSection(string(fixture(t, "sample-prelude.txt")), "GPI SETTINGS"); got != "" {
		t.Fatalf("%q", got)
	}
}

// test_parse_section_missing_header_returns_empty
func TestParseSectionMissingHeaderReturnsEmpty(t *testing.T) {
	if got := ParseSection(string(fixture(t, "sample-prelude.txt")), "NO SUCH SECTION"); got != "" {
		t.Fatalf("%q", got)
	}
}

// test_parse_file_list
func TestParseFileList(t *testing.T) {
	if got := ParseFileList(string(fixture(t, "sample-prelude.txt"))); !slices.Equal(got, []string{"preset-a", "preset-b"}) {
		t.Fatalf("%q", got)
	}
	if got := ParseFileList("Label: x\nEND PRELUDE:\n"); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

// test_extract_label_sanitizes_for_filenames
func TestExtractLabelSanitizesForFilenames(t *testing.T) {
	for in, want := range map[string]string{string(fixture(t, "sample-prelude.txt")): "Keyer_A", "Label: a/b:c d\n": "a_b_c_d",
		"no label here\n": "unknown"} {
		if got := ExtractLabel(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// ---- ranges / annotation ---------------------------------------------------

// test_to_display
func TestToDisplay(t *testing.T) {
	for _, c := range []struct {
		name, raw string
		value     float64
		unit      string
		ok        bool
	}{
		{"Matte Density", "5000", 100, "%", true}, // -100..300, the midpoint
		{"Black Balance", "2500", -50, "%", true}, // -100..100, a quarter
		{"Transition Rate", "30", 30, "frames", true},
		{"Matte Density", "abc", 0, "", false},
		{"Not A Param", "1", 0, "", false},
	} {
		v, u, ok := ToDisplay(c.name, c.raw)
		if v != c.value || u != c.unit || ok != c.ok {
			t.Errorf("%s %s: %v %q %v", c.name, c.raw, v, u, ok)
		}
	}
}

// test_ranges_table_is_well_formed
func TestRangesTableIsWellFormed(t *testing.T) {
	for name, r := range Ranges {
		if r.RawMin > r.RawMax || r.Unit == "" {
			t.Errorf("%s: %+v", name, r)
		}
		if r.DisplayMin != nil && (*r.DisplayMin > *r.DisplayDef || *r.DisplayDef > *r.DisplayMax) {
			t.Errorf("%s: %v %v %v", name, *r.DisplayMin, *r.DisplayDef, *r.DisplayMax)
		}
	}
	if len(Ranges) != 176 {
		t.Errorf("%d ranges", len(Ranges))
	}
}

// test_annotate_preamble_annotates_control_lines_only
func TestAnnotatePreambleAnnotatesControlLinesOnly(t *testing.T) {
	out, err := AnnotatePreamble(string(fixture(t, "sample-prelude.txt")))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	has := func(prefix, suffix string) bool {
		return slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, prefix) && strings.HasSuffix(l, suffix) })
	}
	if !slices.Contains(lines, "Label: Keyer A") || !has("Matte Density: 5000", "(100%)") ||
		!has("Transition Rate: 30", "(30 frames)") || !has("Offset Black Balance: 2500", "(-50%)") ||
		!slices.Contains(lines, "Not A Param: 12") || !slices.Contains(lines, "preset-a") || !strings.HasSuffix(out, "\n") {
		t.Fatalf("%s", out)
	}
}

// ---- binary_request (test_protocol.py) -------------------------------------

func wantIOError(t *testing.T, err error, sub string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Class != "IOError" || !strings.Contains(e.Msg, sub) {
		t.Fatalf("%v (%T), want an IOError with %q", err, err, sub)
	}
}

// test_binary_request_short_length_header_raises_and_closes: 2 of 4
// header bytes, then the unit closes.
func TestBinaryRequestShortLengthHeaderRaisesAndCloses(t *testing.T) {
	s := newScript([]byte{0, 0})
	c, _ := client(s)
	_, err := c.BinaryRequest(ctx, host, 0x0003, []byte("SavedSettings"))
	wantIOError(t, err, "length header")
	if !s.isClosed() {
		t.Error("not closed")
	}
}

// test_binary_request_immediate_eof_raises_and_closes: a reply is owed.
func TestBinaryRequestImmediateEOFRaisesAndCloses(t *testing.T) {
	s := newScript()
	c, _ := client(s)
	_, err := c.BinaryRequest(ctx, host, 0x0003, []byte("GPISettings"))
	wantIOError(t, err, "length header")
	if !s.isClosed() {
		t.Error("not closed")
	}
}

func be32(n int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(n)) }

// test_binary_request_short_body_raises_and_closes: 10 promised, 4 sent.
func TestBinaryRequestShortBodyRaisesAndCloses(t *testing.T) {
	s := newScript(be32(10), []byte("abcd"))
	c, _ := client(s)
	_, err := c.BinaryRequest(ctx, host, 0x0000, []byte("slot1"))
	wantIOError(t, err, "4 of 10")
	if !s.isClosed() {
		t.Error("not closed")
	}
}

// test_binary_request_zero_length_resource_returns_empty
func TestBinaryRequestZeroLengthResourceReturnsEmpty(t *testing.T) {
	s := newScript(be32(0))
	c, _ := client(s)
	if b, err := c.BinaryRequest(ctx, host, 0x0003, []byte("GPISettings")); err != nil || b == nil || len(b) != 0 || !s.isClosed() {
		t.Fatalf("%q %v", b, err)
	}
}

// test_binary_request_full_body_round_trips
func TestBinaryRequestFullBodyRoundTrips(t *testing.T) {
	s := newScript(be32(3), []byte("abc"))
	c, _ := client(s)
	b, err := c.BinaryRequest(ctx, host, 0x0003, []byte("GPISettings"))
	want := append([]byte{0, 3}, append(be32(len("GPISettings")), "GPISettings"...)...)
	if err != nil || string(b) != "abc" || !bytes.Equal(s.written(), want) || !s.isClosed() {
		t.Fatalf("%q %v, sent %x", b, err, s.written())
	}
}

// test_binary_request_accepts_bytes_payload (Go takes bytes only).
func TestBinaryRequestAcceptsBytesPayload(t *testing.T) {
	s := newScript(be32(1), []byte("x"))
	c, _ := client(s)
	b, err := c.BinaryRequest(ctx, host, 0x0000, []byte("slot1"))
	if err != nil || string(b) != "x" || !bytes.HasPrefix(s.written(), append([]byte{0, 0}, be32(5)...)) {
		t.Fatalf("%q %v", b, err)
	}
}

// ---- upload_one ------------------------------------------------------------

func expectedFrame(opcode uint16, name string, data []byte) []byte {
	b := binary.BigEndian.AppendUint16(nil, opcode)
	b = append(append(b, be32(len(name))...), name...)
	return append(append(append(b, be32(len(data))...), data...), 0)
}

// test_upload_one_frame_layout_and_full_ack
func TestUploadOneFrameLayoutAndFullACK(t *testing.T) {
	s := newScript(make([]byte, 6))
	c, _ := client(s)
	if err := c.UploadOne(ctx, host, 0x0100, "presets/slot1", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.written(), expectedFrame(0x0100, "presets/slot1", []byte("payload"))) || !s.isClosed() {
		t.Fatalf("sent %x", s.written())
	}
}

// test_upload_one_tolerates_short_all_zero_ack: the unit closes right
// after the ACK.
func TestUploadOneToleratesShortAllZeroACK(t *testing.T) {
	s := newScript([]byte{0, 0, 0})
	c, _ := client(s)
	if err := c.UploadOne(ctx, host, 0x0103, "GPISettings", []byte("g")); err != nil || !s.isClosed() {
		t.Fatal(err)
	}
}

// test_upload_one_no_ack_bytes_raises
func TestUploadOneNoACKBytesRaises(t *testing.T) {
	s := newScript()
	c, _ := client(s)
	wantIOError(t, c.UploadOne(ctx, host, 0x0103, "GPISettings", []byte("g")), "no ACK")
}

// test_upload_one_nonzero_ack_raises
func TestUploadOneNonzeroACKRaises(t *testing.T) {
	s := newScript([]byte{0, 0, 0, 0, 0, 1})
	c, _ := client(s)
	err := c.UploadOne(ctx, host, 0x0103, "SavedSettings", []byte("s"))
	wantIOError(t, err, "non-zero ACK")
	if !errors.Is(err, ErrRefused) {
		t.Errorf("%v is not ErrRefused", err)
	}
}

// test_upload_one_timeout_waiting_for_ack_raises
func TestUploadOneTimeoutWaitingForACKRaises(t *testing.T) {
	s := newScript(timeout{})
	c, _ := client(s)
	wantIOError(t, c.UploadOne(ctx, host, 0x0100, "presets/slot1", []byte("p")), "timeout waiting for ACK")
	if !s.isClosed() {
		t.Error("not closed")
	}
}

// ---- _connect_with_retry ---------------------------------------------------

// test_connect_with_retry_retries_once_then_succeeds
func TestConnectWithRetryRetriesOnceThenSucceeds(t *testing.T) {
	good := newScript(be32(0))
	c, d := client(errors.New("connection refused"), good)
	if _, err := c.BinaryRequest(ctx, host, 0, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.addrs, []string{host + ":9996", host + ":9996"}) {
		t.Fatalf("%v", d.addrs)
	}
}

// test_connect_with_retry_gives_up_after_second_failure
func TestConnectWithRetryGivesUpAfterSecondFailure(t *testing.T) {
	c, _ := client(errors.New("still down"), errors.New("still down"))
	if _, err := c.ReadPrelude(ctx, host); err == nil || err.Error() != "still down" {
		t.Fatalf("%v", err)
	}
}

// ---- archive (test_profile.py) ---------------------------------------------

var (
	resources = map[string][]byte{"GPISettings": []byte("gpi-bytes"), "SavedSettings": []byte("saved!")}
	slots     = map[string][]byte{"preset-a": []byte("blob-a"), "preset-b": []byte("blob-bb")}
)

// archiveConns are the connections an archive of the sample prelude
// opens: the prelude, each slot, each resource; a name in failing gets a
// connection that closes at once (a short read).
func archiveConns(t *testing.T, failing ...string) []any {
	conns := []any{newScript(fixture(t, "sample-prelude.txt"), timeout{})}
	for _, name := range []string{"preset-a", "preset-b", "GPISettings", "SavedSettings"} {
		blob, ok := slots[name]
		if !ok {
			blob = resources[name]
		}
		if slices.Contains(failing, name) {
			conns = append(conns, newScript())
			continue
		}
		conns = append(conns, newScript(be32(len(blob)), blob))
	}
	return conns
}

var archiveTime = time.Date(2026, 10, 21, 9, 30, 0, 0, time.Local)

func TestArchive(t *testing.T) {
	c, _ := client(archiveConns(t)...)
	z, info, err := c.Archive(ctx, host, archiveTime, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	files := map[string]*zip.File{}
	for _, f := range r.File {
		names = append(names, f.Name)
		files[f.Name] = f
	}
	// test_archive_zip_member_order_matches_smart_remote
	want := []string{"images/", "GPISettings", "SavedSettings", "quickfiles/", "presets/", "presets/preset-a",
		"presets/preset-b", "Keyer_A_config_readable.txt"}
	if !slices.Equal(names, want) {
		t.Fatalf("%q", names)
	}
	content := func(n string) string { b, _ := read(files[n]); return string(b) }
	if content("presets/preset-a") != "blob-a" || content("GPISettings") != "gpi-bytes" ||
		files["presets/"].ExternalAttrs>>16 != 0o40755 || !strings.Contains(content("Keyer_A_config_readable.txt"), "Matte Density: 5000") {
		t.Fatalf("contents")
	}
	// test_archive_info_reports_sizes_and_no_warnings
	if info.Label != "Keyer_A" || !slices.Equal(info.Slots, []string{"preset-a", "preset-b"}) ||
		info.SlotSizes["preset-a"] != 6 || info.SlotSizes["preset-b"] != 7 || info.GPISize != 9 || info.SavedSize != 6 ||
		len(info.Warnings) != 0 {
		t.Fatalf("%+v", info)
	}
}

// test_archive_omits_failed_resource_instead_of_placeholder
func TestArchiveOmitsFailedResourceInsteadOfPlaceholder(t *testing.T) {
	c, _ := client(archiveConns(t, "GPISettings", "preset-b")...)
	z, info, err := c.Archive(ctx, host, archiveTime, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	if slices.Contains(names, "GPISettings") || slices.Contains(names, "presets/preset-b") ||
		!slices.Contains(names, "SavedSettings") || !slices.Contains(names, "presets/preset-a") {
		t.Fatalf("%q", names)
	}
	if _, ok := info.SlotSizes["preset-b"]; ok || info.GPISize != 0 || len(info.Warnings) != 2 {
		t.Fatalf("%+v", info)
	}
}

// test_archive_empty_preamble_is_a_hard_failure
func TestArchiveEmptyPreambleIsAHardFailure(t *testing.T) {
	c, _ := client(newScript())
	_, _, err := c.Archive(ctx, host, archiveTime, nil)
	wantIOError(t, err, "empty preamble")
}

// ---- restore ---------------------------------------------------------------

func zipOf(t *testing.T, members ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := 0; i < len(members); i += 2 {
		f, err := w.Create(members[i])
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(f, members[i+1])
	}
	w.Close()
	return buf.Bytes()
}

// pushed is the opcode, the name's last segment and the data of each
// write frame sent.
func pushed(t *testing.T, conns []*script) []string {
	var out []string
	for _, s := range conns {
		b := s.written()
		if len(b) == 0 {
			continue
		}
		op := binary.BigEndian.Uint16(b)
		n := binary.BigEndian.Uint32(b[2:])
		name := string(b[6 : 6+n])
		data := b[10+n : len(b)-1]
		seg := name[strings.LastIndex(name, "/")+1:]
		out = append(out, strings.Join([]string{"0x" + string("0123456789abcdef"[op>>8&0xf]) + string("0123456789abcdef"[op&0xf]), seg, string(data)}, " "))
		if op == OpWriteSlot && !strings.Contains(name, "/presets/") {
			t.Errorf("slot name %q", name)
		}
	}
	return out
}

func acks(n int) ([]any, []*script) {
	var conns []any
	var ss []*script
	for range n {
		s := newScript(make([]byte, 6))
		conns, ss = append(conns, s), append(ss, s)
	}
	return conns, ss
}

// test_restore_pushes_presets_then_gpi_then_saved_settings
func TestRestorePushesPresetsThenGPIThenSavedSettings(t *testing.T) {
	conns, ss := acks(4)
	c, _ := client(conns...)
	z := zipOf(t, "SavedSettings", "S", "presets/", "", "presets/zeta", "Z", "presets/alpha", "A", "GPISettings", "G",
		"notes.txt", "ignored")
	ok, info, err := c.Restore(ctx, host, z, nil)
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if got := pushed(t, ss); !slices.Equal(got, []string{"0x10 alpha A", "0x10 zeta Z", "0x13 GPISettings G", "0x13 SavedSettings S"}) {
		t.Fatalf("%q", got)
	}
	if len(info.Presets) != 2 || info.Presets[0] != (PresetPushed{"alpha", Pushed{OK: true}}) || info.GPI == nil || !info.GPI.OK ||
		info.Saved == nil || !info.Saved.OK || info.StoppedAt != nil {
		t.Fatalf("%+v", info)
	}
}

// test_restore_aborts_at_first_failure
func TestRestoreAbortsAtFirstFailure(t *testing.T) {
	bad := newScript([]byte{1, 0, 0, 0, 0, 0})
	good := newScript(make([]byte, 6))
	c, d := client(good, bad)
	z := zipOf(t, "presets/alpha", "A", "presets/beta", "B", "GPISettings", "G", "SavedSettings", "S")
	ok, info, err := c.Restore(ctx, host, z, nil)
	if err != nil || ok || info.StoppedAt == nil || *info.StoppedAt != "preset:beta" {
		t.Fatalf("%v %+v %v", ok, info, err)
	}
	if info.Presets[1].OK || !strings.Contains(*info.Presets[1].Error, "non-zero ACK") || info.GPI != nil || info.Saved != nil {
		t.Fatalf("%+v", info)
	}
	if len(d.addrs) != 2 {
		t.Errorf("%d connections: nothing after the failure is tried", len(d.addrs))
	}
}

// test_restore_skips_absent_resources
func TestRestoreSkipsAbsentResources(t *testing.T) {
	conns, _ := acks(1)
	c, d := client(conns...)
	ok, info, err := c.Restore(ctx, host, zipOf(t, "presets/only", "O"), nil)
	if err != nil || !ok || len(d.addrs) != 1 || info.GPI != nil || info.Saved != nil {
		t.Fatalf("%v %+v %v %v", ok, info, err, d.addrs)
	}
}

// ---- the network interface (test_network.py) -------------------------------

// wired is the test's fixture: one session per reply, each the prelude,
// the reply and a timeout; a set is two sessions (the write, then the
// read-back), the last reply repeated for any more.
func wired(t *testing.T, replies ...string) (*Client, *[]*script) {
	t.Helper()
	socks := &[]*script{}
	c, _ := client()
	pre := fixture(t, "network-prelude.bin")
	c.DialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
		reply := replies[min(len(*socks), len(replies)-1)]
		s := newScript(pre, []byte(reply), timeout{})
		*socks = append(*socks, s)
		return s, nil
	}
	return c, socks
}

func echo(t *testing.T) string { return string(fixture(t, "network-echo.txt")) }

func wantNetworkError(t *testing.T, err error, sub string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Class != "UltimatteNetworkError" || !strings.Contains(e.Msg, sub) {
		t.Fatalf("%v (%T), want an UltimatteNetworkError with %q", err, err, sub)
	}
}

func str(s string) *string { return &s }

// test_a_query_block_is_the_header_and_the_submitting_blank_line
func TestAQueryBlockIsTheHeaderAndTheSubmittingBlankLine(t *testing.T) {
	if b := BuildBlock(0); b != "NETWORK INTERFACE 0:\n\n" {
		t.Fatalf("%q", b)
	}
}

// test_a_set_block_ends_with_the_blank_line_that_submits_it
func TestASetBlockEndsWithTheBlankLineThatSubmitsIt(t *testing.T) {
	if b := BuildBlock(0, Field{"Static Gateway", "192.0.2.1"}); b != "NETWORK INTERFACE 0:\nStatic Gateway: 192.0.2.1\n\n" {
		t.Fatalf("%q", b)
	}
}

// test_the_index_selects_the_section
func TestTheIndexSelectsTheSection(t *testing.T) {
	if SectionName(2) != "NETWORK INTERFACE 2" || !strings.HasPrefix(BuildBlock(2), "NETWORK INTERFACE 2:") {
		t.Fatal(SectionName(2))
	}
}

func parse(t *testing.T, text string) NetworkInterface {
	t.Helper()
	iface, err := ParseInterface(text, 0)
	if err != nil {
		t.Fatal(err)
	}
	return iface
}

// test_parse_splits_the_combined_address_field
func TestParseSplitsTheCombinedAddressField(t *testing.T) {
	i := parse(t, echo(t))
	if i.Address != "192.0.2.98" || i.Netmask != "255.255.252.0" || i.StaticAddress != "192.0.2.98" || i.StaticNetmask != "255.255.252.0" {
		t.Fatalf("%+v", i)
	}
}

// test_parse_reads_the_rest_of_the_section
func TestParseReadsTheRestOfTheSection(t *testing.T) {
	i := parse(t, echo(t))
	if i.MAC != "7c:2e:0d:00:00:01" || i.Name != "BMD 10G Ethernet MAC" || i.Gateway != "192.0.2.1" || i.Dynamic == nil ||
		*i.Dynamic || i.Priority == nil || *i.Priority != 0 || len(i.DNS) != 0 || len(i.StaticDNS) != 0 {
		t.Fatalf("%+v", i)
	}
}

// test_dynamic_ip_true_is_read_as_dhcp
func TestDynamicIPTrueIsReadAsDHCP(t *testing.T) {
	if i := parse(t, strings.Replace(echo(t), "Dynamic IP: false", "Dynamic IP: true", 1)); i.Dynamic == nil || !*i.Dynamic {
		t.Fatal(i.Dynamic)
	}
}

// test_an_absent_dynamic_flag_is_unknown_not_false
func TestAnAbsentDynamicFlagIsUnknownNotFalse(t *testing.T) {
	if i := parse(t, strings.Replace(echo(t), "Dynamic IP: false\n", "", 1)); i.Dynamic != nil {
		t.Fatal(*i.Dynamic)
	}
}

// test_an_address_without_a_mask_does_not_raise
func TestAnAddressWithoutAMaskDoesNotRaise(t *testing.T) {
	if i := parse(t, strings.ReplaceAll(echo(t), "192.0.2.98/255.255.252.0", "192.0.2.98")); i.Address != "192.0.2.98" || i.Netmask != "" {
		t.Fatalf("%+v", i)
	}
}

// test_multiple_addresses_take_the_first
func TestMultipleAddressesTakeTheFirst(t *testing.T) {
	i := parse(t, strings.Replace(echo(t), "Current Addresses: 192.0.2.98/255.255.252.0",
		"Current Addresses: 192.0.2.98/255.255.252.0, 10.0.0.5/255.0.0.0", 1))
	if i.Address != "192.0.2.98" || i.Netmask != "255.255.252.0" {
		t.Fatalf("%+v", i)
	}
}

// test_dns_servers_parse_as_a_list
func TestDNSServersParseAsAList(t *testing.T) {
	i := parse(t, strings.Replace(echo(t), "Static DNS Servers:", "Static DNS Servers: 8.8.8.8 8.8.4.4", 1))
	if !slices.Equal(i.StaticDNS, []string{"8.8.8.8", "8.8.4.4"}) {
		t.Fatalf("%q", i.StaticDNS)
	}
}

// test_read_network_queries_and_parses: a read sets nothing.
func TestReadNetworkQueriesAndParses(t *testing.T) {
	c, socks := wired(t, "ACK\n\n"+echo(t))
	i, err := c.ReadNetwork(ctx, host, 0)
	if err != nil || i.Netmask != "255.255.252.0" || string((*socks)[0].written()) != "NETWORK INTERFACE 0:\n\n" || !(*socks)[0].isClosed() {
		t.Fatalf("%+v %v", i, err)
	}
}

// test_a_nak_is_an_error_not_an_empty_result
func TestANakIsAnErrorNotAnEmptyResult(t *testing.T) {
	c, _ := wired(t, "NAK\n\n")
	_, err := c.ReadNetwork(ctx, host, 0)
	wantNetworkError(t, err, "NAK")
}

// test_silence_is_an_error
func TestSilenceIsAnError(t *testing.T) {
	c, _ := wired(t, "")
	_, err := c.ReadNetwork(ctx, host, 0)
	wantNetworkError(t, err, "no ACK")
}

var (
	setACK     = "ACK\n\nNETWORK INTERFACE 0:\nStatic Addresses: x\n\n"
	widenedSet = NetworkSet{Address: str("192.0.2.98"), Netmask: str("255.255.248.0")}
)

// test_set_sends_address_and_mask_as_one_field
func TestSetSendsAddressAndMaskAsOneField(t *testing.T) {
	widened := strings.ReplaceAll(echo(t), "255.255.252.0", "255.255.248.0")
	c, socks := wired(t, setACK, "ACK\n\n"+widened)
	i, err := c.SetNetwork(ctx, host, 0, widenedSet)
	sent := string((*socks)[0].written())
	if err != nil || !strings.Contains(sent, "Static Addresses: 192.0.2.98/255.255.248.0\n") || !strings.HasSuffix(sent, "\n\n") ||
		i.StaticNetmask != "255.255.248.0" {
		t.Fatalf("%+v %v %q", i, err, sent)
	}
}

// test_set_verifies_by_reading_back_not_by_trusting_the_echo
func TestSetVerifiesByReadingBackNotByTrustingTheEcho(t *testing.T) {
	c, socks := wired(t, setACK, "ACK\n\n"+echo(t))
	c.ReadbackTimeout = -1
	c.SetNetwork(ctx, host, 0, widenedSet)
	if len(*socks) != 2 || string((*socks)[1].written()) != "NETWORK INTERFACE 0:\n\n" {
		t.Fatalf("%d sessions", len(*socks))
	}
}

// test_set_returns_what_the_unit_says_not_what_was_asked: if the unit
// keeps the old value, the caller must be able to see that (current and
// static both the old mask: settled).
func TestSetReturnsWhatTheUnitSaysNotWhatWasAsked(t *testing.T) {
	c, _ := wired(t, setACK, "ACK\n\n"+echo(t))
	i, err := c.SetNetwork(ctx, host, 0, widenedSet)
	if err != nil || i.StaticNetmask != "255.255.252.0" {
		t.Fatalf("%+v %v", i, err)
	}
}

func reapplying(t *testing.T) string {
	return strings.Replace(strings.Replace(echo(t), "Current Addresses: 192.0.2.98/255.255.252.0",
		"Current Addresses: 0.0.0.0/255.255.0.0", 1), "Static Addresses: 192.0.2.98/255.255.252.0",
		"Static Addresses: 192.0.2.98/255.255.248.0", 1)
}

// test_a_mid_reapply_read_is_not_accepted_as_settled
func TestAMidReapplyReadIsNotAcceptedAsSettled(t *testing.T) {
	settledEcho := strings.ReplaceAll(echo(t), "255.255.252.0", "255.255.248.0")
	c, _ := wired(t, setACK, "ACK\n\n"+reapplying(t), "ACK\n\n"+settledEcho)
	i, err := c.SetNetwork(ctx, host, 0, widenedSet)
	if err != nil || i.Address != "192.0.2.98" || i.Netmask != "255.255.248.0" {
		t.Fatalf("%+v %v", i, err)
	}
}

// test_an_interface_that_never_settles_raises
func TestAnInterfaceThatNeverSettlesRaises(t *testing.T) {
	c, _ := wired(t, setACK, "ACK\n\n"+reapplying(t))
	c.ReadbackTimeout = -1
	_, err := c.SetNetwork(ctx, host, 0, widenedSet)
	wantNetworkError(t, err, "did not settle")
	if !errors.Is(err, ErrUnsettled) {
		t.Errorf("%v isn't ErrUnsettled", err)
	}
}

// test_current_must_match_static_before_a_set_is_believed
func TestCurrentMustMatchStaticBeforeASetIsBelieved(t *testing.T) {
	stale := strings.Replace(echo(t), "Static Addresses: 192.0.2.98/255.255.252.0", "Static Addresses: 192.0.2.98/255.255.248.0", 1)
	c, _ := wired(t, setACK, "ACK\n\n"+stale)
	c.ReadbackTimeout = -1
	_, err := c.SetNetwork(ctx, host, 0, widenedSet)
	wantNetworkError(t, err, "did not settle")
}

// test_a_set_that_touches_no_address_does_not_wait_for_the_interface
func TestASetThatTouchesNoAddressDoesNotWaitForTheInterface(t *testing.T) {
	c, socks := wired(t, setACK, "ACK\n\n"+echo(t))
	i, err := c.SetNetwork(ctx, host, 0, NetworkSet{Gateway: str("192.0.2.1")})
	if err != nil || i.StaticGateway != "192.0.2.1" || len(*socks) != 2 {
		t.Fatalf("%+v %v %d", i, err, len(*socks))
	}
}

// test_a_write_that_cannot_be_read_back_raises_loudly: accepted, then
// unreachable, is never a success.
func TestAWriteThatCannotBeReadBackRaisesLoudly(t *testing.T) {
	c, _ := wired(t, setACK, "")
	c.ReadbackTimeout = -1
	_, err := c.SetNetwork(ctx, host, 0, widenedSet)
	wantNetworkError(t, err, "CHECK THIS UNIT")
}

// test_an_address_without_its_mask_is_refused_before_the_wire
func TestAnAddressWithoutItsMaskIsRefusedBeforeTheWire(t *testing.T) {
	c, socks := wired(t, "ACK\n\n"+echo(t))
	_, err := c.SetNetwork(ctx, host, 0, NetworkSet{Address: str("192.0.2.98")})
	if !errors.Is(err, ErrValue) || !strings.Contains(err.Error(), "one field") || len(*socks) != 0 {
		t.Fatalf("%v, %d sessions", err, len(*socks))
	}
}

// test_a_mask_without_its_address_is_refused_too
func TestAMaskWithoutItsAddressIsRefusedToo(t *testing.T) {
	c, _ := wired(t, "")
	if _, err := c.SetNetwork(ctx, host, 0, NetworkSet{Netmask: str("255.255.248.0")}); !strings.Contains(fmtErr(err), "one field") {
		t.Fatal(err)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// test_setting_nothing_is_refused
func TestSettingNothingIsRefused(t *testing.T) {
	c, _ := wired(t, "")
	if _, err := c.SetNetwork(ctx, host, 0, NetworkSet{}); fmtErr(err) != "nothing to set" {
		t.Fatal(err)
	}
}

// test_gateway_and_dns_can_be_set_alone
func TestGatewayAndDNSCanBeSetAlone(t *testing.T) {
	c, socks := wired(t, "ACK\n\n"+echo(t))
	c.SetNetwork(ctx, host, 0, NetworkSet{Gateway: str("192.0.2.1"), DNS: &[]string{"8.8.8.8", "1.1.1.1"}})
	sent := string((*socks)[0].written())
	if !strings.Contains(sent, "Static Gateway: 192.0.2.1\n") || !strings.Contains(sent, "Static DNS Servers: 8.8.8.8 1.1.1.1\n") ||
		strings.Contains(sent, "Static Addresses") {
		t.Fatalf("%q", sent)
	}
}

// test_dns_empty_list_clears_and_none_leaves_alone
func TestDNSEmptyListClearsAndNoneLeavesAlone(t *testing.T) {
	c, socks := wired(t, "ACK\n\n"+echo(t))
	c.SetNetwork(ctx, host, 0, NetworkSet{DNS: &[]string{}})
	if !strings.Contains(string((*socks)[0].written()), "Static DNS Servers: \n") {
		t.Fatalf("%q", (*socks)[0].written())
	}
	c2, socks2 := wired(t, "ACK\n\n"+echo(t))
	c2.SetNetwork(ctx, host, 0, NetworkSet{Gateway: str("192.0.2.1")})
	if strings.Contains(string((*socks2)[0].written()), "Static DNS Servers") {
		t.Fatalf("%q", (*socks2)[0].written())
	}
}

// test_switching_to_dhcp_is_expressible
func TestSwitchingToDHCPIsExpressible(t *testing.T) {
	c, socks := wired(t, "ACK\n\n"+echo(t))
	c.SetNetwork(ctx, host, 0, NetworkSet{Dynamic: new(true)})
	if !strings.Contains(string((*socks)[0].written()), "Dynamic IP: true\n") {
		t.Fatalf("%q", (*socks)[0].written())
	}
}

// test_a_refused_set_raises_rather_than_reporting_success
func TestARefusedSetRaisesRatherThanReportingSuccess(t *testing.T) {
	c, _ := wired(t, "NAK\n\n")
	_, err := c.SetNetwork(ctx, host, 0, widenedSet)
	wantNetworkError(t, err, "NAK")
}
