# `.golangci.yml` must declare the v2 config schema

**File:** `.golangci.yml`

## What the generator emits

A config with a v2-only `formatters:` section but no `version` key.

## What this CLI needs

```
version: "2"
```

as the first line.

## Why

golangci-lint v2 refuses a config without `version: "2"`
("unsupported version of the configuration: \"\""), so `make lint` fails before
linting anything.

## Reprint check

`head -1 .golangci.yml`. If it is not `version: "2"`, add it back.

## Upstream

The emitted config should carry the schema version it is written in.
