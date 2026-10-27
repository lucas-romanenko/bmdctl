# ultimatte test data

MIT, as bmdwire's ultimattewire is.

## fixtures/

The texts bmdwire v1.6.0's `ultimattewire/tests` (tag `v1.6.0`, commit
`0eb73e5`) script their fakes with, copied raw; v1.7.0 added none (its
12 4K prelude is inline in `test_preamble.py`, and in `client_test.go`). They are synthetic: the
tests write them by hand, in the unit's shape. `client_test.go` ports the
tests on them, and `oracle/gen_oracle.py` reads them from the tests
themselves.

| file | bytes | origin |
|---|---|---|
| `sample-prelude.txt` | 188 | `conftest.py`, `SAMPLE_PRELUDE`: a label, a release, CONTROL values, a FILE LIST of two presets, an empty section, END PRELUDE |
| `network-prelude.bin` | 46 | `test_network.py`, `PRELUDE`: the prelude the network tests' sessions open with |
| `network-echo.txt` | 292 | `test_network.py`, `ECHO`: NETWORK INTERFACE 0 as an Ultimatte 12 4K on protocol 2.1 echoes a query (addresses made up) |

## oracle/

`oracle.json` is what ultimattewire v1.7.0 itself does, recorded by
running it: RANGES; 4,041 parser cases (`parse_section` on seven headers,
`parse_file_list`, `extract_label`, `extract_model`,
`parse_current_file` and `annotate_preamble` on 60 preludes, CR LF, odd labels, sections that end every way and values of
every form among them; `parse_interface` on 17 echoes; `to_display` on
every range with 17 values; `build_block`); and 268 calls over scripted
connections (`binary_request`, `upload_one`, `read_preamble`, `exchange`,
`read_network`, `set_network` with its read-back and a clock that moves
only when it sleeps, `archive_unit_to_bytes`, `restore_unit_from_bytes`),
each with its result or exception and every connection's address and
bytes sent. An archive's zip is recorded member by member: name, method,
attributes, flags, versions, CRC, size and contents. `TestRangesMatchUltimattewire`,
`TestParsersMatchUltimattewire` and `TestCallsMatchUltimattewire`
replay them all. Regenerate against a bmdwire checkout at `v1.7.0` (the
seed is fixed, so the file changes only when bmdwire or the script does):

```
cd ultimatte/testdata/oracle
python3 -I gen_oracle.py ../../../../bmdwire oracle.json
```
