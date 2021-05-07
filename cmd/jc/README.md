## JC

This is a command line tool which can convert JSON to CBOR and vice versa, with support for Matrix key enums. This utility
always produces canonical output for deterministic behaviour.

```bash
./jc '{"foo":"bar"}'

Output to 'output' (9 bytes) a163666f6f63626172
```

Matrix key enum `event_id` is replaced with byte `0x01`.
```bash
./jc -out '-' '{"event_id":"$something"}'

?j$something%
```

Round trip:
```bash
./jc -out '-' '{"event_id":"$something", "foo":"bar"}' |
./jc -c2j -out '-' '-'

{"event_id":"$something","foo":"bar"}
```

For the full list of Matrix key enums, see [MSC3079](https://github.com/matrix-org/matrix-doc/blob/kegan/low-bandwidth/proposals/3079-low-bandwidth-csapi.md#appendix-a-cbor-integer-keys).