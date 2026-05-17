# cli

A TUI app drawer for your command-line tools. Lists all executables on your PATH, lets you star favorites, add descriptions, filter with fuzzy search, and select a command to place on your command line.

## Install

```bash
go build -o cli .
cp cli ~/.local/bin/
```

## Shell Integration

Add this to your `~/.bashrc` so that selecting a command places it on your command line.
`READLINE_LINE` only works inside `bind -x`, so we bind **Ctrl+G** to launch:

```bash
function __cli_launch() {
	command cli "$@"
	local sel="${XDG_CACHE_HOME:-$HOME/.cache}/cli/selection"
	if [ -f "$sel" ]; then
		READLINE_LINE=$(cat "$sel")
		READLINE_POINT=${#READLINE_LINE}
		rm -f "$sel"
	fi
}
bind -x '"\C-g": __cli_launch'
alias cli='command cli'
```

For zsh, add this to `~/.zshrc` instead:

```zsh
cli() {
  command cli "$@"
  local sel="${XDG_CACHE_HOME:-$HOME/.cache}/cli/selection"
  if [ -f "$sel" ]; then
    print -z "$(cat "$sel")"
    rm -f "$sel"
  fi
}
```

Then restart your shell or run `source ~/.bashrc`.

## Usage

Launch with `cli`, then:

| Key | Action |
|-----|--------|
| `↑↓` or `j/k` | Navigate |
| `/` | Filter (fuzzy search by name and description) |
| `s` | Toggle star (starred items appear at the top) |
| `d` | Add/edit description for the selected command |
| `Enter` | Select command and place it on your command line |
| `g` / `G` | Jump to top / bottom |
| `q` / `Esc` | Quit |

## Config

Stars and descriptions are stored in `~/.config/cli/config.json`.

## Upgrading

`cli` can replace itself with the latest GitHub release (linux/amd64):

```bash
cli -upgrade    # download + verify + replace the running binary
cli -version    # print the installed version
```

Upgrade is powered by [selfupgrade](https://github.com/krzysztofciepka/selfupgrade).
Release builds embed the version via `-ldflags "-X main.version=<tag>"`; the
release process must continue to publish `cli-<tag>-linux-amd64` and
`checksums.txt` assets.
