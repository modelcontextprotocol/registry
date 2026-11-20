# Usage collector fixtures

Everything in this directory is synthetic.

- Source addresses come only from the ranges reserved for documentation: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` and `2001:db8::/32`. `TestFixturesAreSynthetic` enforces this.
- Network numbers are from the ranges reserved for documentation and private use. Network descriptions imitate the style of real registrations so that the naming rules are exercised.
- Organizations, products, their user agents, server names and request IDs are made up. The organizations are fictional companies. Hosting providers and HTTP libraries keep their real names, because recognising those is what the classification rules do.
- The traffic mix is arbitrary. It describes no real system.

## Files

| Path | Contents |
| --- | --- |
| `entries/typical.jsonl` | A day of access records, one Cloud Logging entry per line. It mirrors the storage rule: about 1 in 10 fast successful listing records are present, and every other record is. |
| `entries/typical.counters.json` | The gateway counters for that day, covering all of its traffic. |
| `entries/hostile.jsonl` | Records with malformed and unusual fields. Read this one to see which inputs are covered. |
| `networks/` | A small address-range table and network-type list in the formats of the public files. |
| `catalogue.json` | Published server versions, for the server lookup breakdowns. |
| `scenarios/<name>/scenario.json` | Which of the above a scenario uses, and what is unavailable in it. |
| `scenarios/<name>/report.md`, `rows.json` | The expected report and rows for the scenario. |
| `prototype_cases.json` | Inputs for single functions, each followed by the expected result. |

## Where the expected output comes from

This package replaces a Python prototype: a report script and a wrapper that extracted more detail from it. The expected output was produced by that prototype, run on these fixtures with its inputs replaced: the counter queries, the log read, and the two network reference files.

One more thing was replaced, in memory only. The prototype named an organization's networks in its own table; this package takes such aliases from a setting. For each scenario the prototype's table was therefore set to the scenario's aliases followed by its hosting providers.

- `report.md` is the prototype's report. The tests require the same bytes.
- `rows.json` is the wrapper's output: one row per line, fields that are empty left out. The tests require the same rows with the same figures, digit for digit. A scenario has no `rows.json` when its rows are the same as another scenario's and only its report differs.
- `prototype_cases.json` is what the prototype's own functions returned for each input.

The prototype is retired, so these files are not regenerated from it. When the figures are meant to change, run:

```bash
go test ./internal/usage -run TestGoldenParity -update
```

and review the difference. On the current code this rewrites every file to exactly what it already is.
