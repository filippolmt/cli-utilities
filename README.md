# cli-utilities

Command-line tools for self-hosted services, one Go CLI per directory. Each CLI
is generated with [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
and ships with an MCP server and an agent skill.

| CLI | What it drives |
| --- | --- |
| [`changedetection-pp-cli`](changedetection-pp-cli/) | A self-hosted [changedetection.io](https://changedetection.io) instance: watches, tags, notifications, recent changes and diffs. |
| [`fritzbox-pp-cli`](fritzbox-pp-cli/) | An AVM FRITZ!Box router over TR-064, with a local SQLite history of hosts, WAN and the system log. |

## Build

Each CLI is a separate Go module. Build it from its own directory:

```bash
cd fritzbox-pp-cli
make build        # binary in bin/
make build-mcp    # MCP server
```

Credentials stay in the CLI's directory: copy `config.toml.example` to
`config.toml` (or `.env.template` to `.env`, where present) and fill it in.
Each CLI's README covers setup and commands.

## License

[MIT](LICENSE)
