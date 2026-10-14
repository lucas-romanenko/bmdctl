# hyperdeck test data

MIT, as bmdwire's hyperdeckwire is. (`internal/fixtures/` holds atemwire's
data, which is LGPL-3.0-only; nothing of hyperdeckwire's goes there.)

## fixtures/

Every byte string bmdwire v1.3.0's `hyperdeckwire/tests/test_hyperdeckwire_client.py`
(tag `v1.3.0`, commit `cea58c2`) scripts its fake socket with, copied raw.
They are synthetic: the tests write them by hand, in the deck's form.
`client_test.go` ports the tests on them, and `oracle/gen_client.py` runs
hyperdeckwire on them.

| file | bytes | origin |
|---|---|---|
| `greeting.bin` | 81 | `GREETING`, every test |
| `ok.bin` | 8 | `test_simple_ack_response_is_ok_no_lines` and the command tests |
| `device-info.bin` | 142 | `test_multi_line_response_terminates_on_blank_line` |
| `remote-disabled.bin` | 29 | `test_error_response_raises_hyperdeck_error_with_code` |
| `async-transport-stopped-then-ok.bin` | 48 | `test_async_message_skipped_so_real_response_returns` |
| `async-transport-play.bin` | 37 | `test_async_message_returned_when_skip_async_false` |
| `disk-list.bin` | 131 | `test_disk_list_filters_header_lines_and_returns_clips` |
| `invalid-value.bin` | 19 | `test_set_configuration_raises_on_protocol_error` |
| `deck-rebooting.bin` | 20 | `test_set_configuration_accepts_213_deck_rebooting_as_success` |
| `clips-count.bin` | 35 | `test_clips_count_returns_int` |

## oracle/

`client.json` is what hyperdeckwire v1.3.0 itself does, recorded by running
its real `Hyperdeck` class over a scripted socket: 747 sessions (the tests'
scripts on the fixtures; every method against every kind of answer; 19
greetings; listings of every kind of row; desynchronising streams), 872
calls, each with the bytes sent, how far into the stream it had read, and
the result or the exception; 132 cases of the three line parsers and 16 of
`Response.params`. `TestClientMatchesHyperdeckwire` and
`TestParsersMatchHyperdeckwire` replay them all. Regenerate against a
bmdwire checkout at `v1.3.0` (the seed is fixed, so the file changes only
when bmdwire or the script does). It stays at v1.3.0 while the port
follows v1.4.0: hyperdeckwire 1.4.0 drops an answer already buffered when
it sends a request (bmdwire#19, #32), and this script's socket hands the
client the whole session on its first read, so at 1.4.0 it would record
that drop instead of the client's work (`docs/PORTING.md`, "HyperDeck",
Verification):

```
cd hyperdeck/testdata/oracle
python3 -I gen_client.py ../../../../bmdwire ../fixtures client.json
```
