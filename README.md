# mootd

`mootd` prints a new greeting each time you open a terminal: a small piece of colour text art, and a joke about today's news in a speech bubble. Claude writes a fresh batch every day. It is `fortune | cowsay | lolcat` for people who already pay for Claude Code.

```
  ╭─────────────────────────────────────────────────────────╮
  │ Status report: heard 412 bug explanations this quarter. │
  │ Said nothing. All 412 resolved. Requesting a promotion  │
  │ and a bigger bath.                                      │
  ╰───┬─────────────────────────────────────────────────────╯
      │
      __
    <(o )___
     ( ._> /
      `---'
```

## What you need

- macOS or Linux, with zsh or bash.
- [Claude Code](https://claude.com/claude-code), installed and signed in. mootd writes its greetings through the `claude` command, so it uses your existing plan and needs no API key.

## Install

With Homebrew, on macOS or Linux:

```sh
brew install joeuk89/tap/mootd
mootd init
```

Without Homebrew:

```sh
curl -fsSL https://raw.githubusercontent.com/joeuk89/mootd/main/install.sh | sh
mootd init
```

`mootd init` checks that Claude Code is signed in, writes a config file, and asks before adding one line to your shell startup file. If you have more than one Claude account, it asks which one to use.

## Commands

| Command | What it does |
| --- | --- |
| `mootd` | Print the next greeting |
| `mootd keep` | Keep this window's greeting for good, and ask for more like it |
| `mootd nope [reason]` | Drop this window's greeting, and ask for fewer like it |
| `mootd open` | Open this window's news story in the browser |
| `mootd generate` | Make a new batch now |
| `mootd status` | Show the pool, the last generation and any errors |
| `mootd config` | Edit the settings |
| `mootd uninstall` | Remove the shell hook; add `--purge` to delete settings and greetings too |

`keep` and `nope` teach it your taste. The next batch is written with your 15 latest favourites and 15 latest rejects in the prompt. A reason helps: `mootd nope too many AI jokes`.

## How it works

1. **The first terminal you open each day starts a generation in the background.** It takes about six minutes. Until it finishes, new terminals show greetings from earlier days or the built-in set.
2. **The generation reads the news.** It fetches RSS feeds for programming, international, culture and UK news.
3. **Claude writes more greetings than needed.** A second call scores each one for humour and for how well the art reads, and mootd keeps the best in each category.
4. **Printing is fast.** Showing a greeting takes about 15 milliseconds, because it only reads a file.

Greetings about the news stay in rotation for three days. The newest 200 timeless ones stay too; older ones drop out as new ones arrive. Greetings you keep, and the built-in set, stay for good. You see every unseen greeting before anything repeats.

## Settings

Run `mootd config` to edit `~/.config/mootd/config.toml`. The file lists every setting with its default. The ones you are most likely to change:

| Setting | Default | What it does |
| --- | --- | --- |
| `model` | `claude-opus-5-5` | The model that writes and scores greetings |
| `effort` | model default | `low` is three times faster and cheaper, but draws plainer art |
| `[mix]` | 4 per category | How many greetings to keep per category each day |
| `evergreen_limit` | 200 | How many timeless greetings stay in rotation |
| `[[feeds]]` | nine news feeds | The RSS or Atom feeds to read, each with a category |
| `style`, `interests` | empty | Steer the humour in your own words |
| `skip_terminals` | none | Terminal programs that should stay quiet, such as `vscode` |
| `claude_config_dir` | Claude's default | Which Claude account to use |

Categories are yours to define. To get football jokes, add a feed with `category = "football"` and a line `football = 4` under `[mix]`.

Set `MOOTD_SKIP=1` to silence mootd in one shell. It also respects `NO_COLOR`.

## What it costs

A daily batch on the default settings uses about 35,000 output tokens, drawn from your Claude plan's usage. That is roughly $0.85 a day at API prices. To cut it, set `effort = "low"`, lower the `[mix]` counts, or set `cull = false`.

## Safety

Headlines are text from the internet, and they go into a prompt. mootd limits what that can do:

- Every `claude` call runs with tools, MCP servers, hooks and settings files switched off. The model can only return text.
- Every greeting is checked before it is stored. Terminal escape codes, emoji and links that are not `http` or `https` are rejected.
- The prompt tells the model to skip stories about death, war, disaster and private misfortune. The scoring call drops any joke that slips through.

## Where things live

| Path | Holds |
| --- | --- |
| `~/.config/mootd/config.toml` | Your settings |
| `~/.local/share/mootd/pool.json` | Greetings in rotation |
| `~/.local/share/mootd/archive.jsonl` | Every greeting ever added |
| `~/.local/share/mootd/feedback.jsonl` | Your keeps and nopes |
| `~/.local/share/mootd/logs/` | Generation logs, kept for 14 days |

Nothing leaves your machine except the feed requests and the calls to Claude.

## Uninstall

```sh
mootd uninstall --purge
brew uninstall mootd
```

## Build from source

```sh
git clone https://github.com/joeuk89/mootd && cd mootd
go build -o bin/mootd ./cmd/mootd
go test ./...
```

## Licence

MIT.
