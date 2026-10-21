# videohub test data

MIT, as bmdwire's videohubwire is.

## fixtures/

The two byte strings bmdwire v1.6.0's
`videohubwire/tests/test_videohubwire_client.py` (tag `v1.6.0`, commit
`0eb73e5`) scripts its fake socket with, copied raw. They are synthetic: the
tests write them by hand, in a Smart Videohub 40 x 40's shape on protocol
2.7. `client_test.go` ports the tests on them, and `oracle/gen_client.py`
reads them from the tests themselves.

| file | bytes | origin |
|---|---|---|
| `preamble.bin` | 458 | `PREAMBLE`, every test: the protocol, device, labels, locks and routing blocks of a 4 x 4 hub |
| `end-prelude.bin` | 14 | `END_PRELUDE`, the marker newer firmware ends the preamble with |

## oracle/

`client.json` is what videohubwire v1.6.0 itself does, recorded by running
its real `Videohub` class over a scripted socket: 213 sessions, 646 steps
(the tests' preamble whole, without its marker and in pieces; 24 preambles
that fail or end oddly, CR LF and stray blank lines among them; every
command against ten kinds of reply; the refusals before the wire on hubs
with good, missing, bad and negative counts; 16 labels in both setters;
pushes between calls; close; every command timing out; 60 random hubs and
call sequences). Each step records the bytes sent, the result or the
exception, the six attributes after it and how much of the stream it
hadn't read. `TestClientMatchesVideohubwire` replays them all. Regenerate
against a bmdwire checkout at `v1.6.0` (the seed is fixed, so the file
changes only when bmdwire or the script does):

```
cd videohub/testdata/oracle
python3 -I gen_client.py ../../../../bmdwire client.json
```
