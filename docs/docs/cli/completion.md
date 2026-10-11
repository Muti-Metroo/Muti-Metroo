---
title: muti-metroo completion
sidebar_label: completion
sidebar_position: 26
description: Generate shell completion scripts for bash, zsh, fish, and PowerShell.
---

# muti-metroo completion

Print a shell completion script for `muti-metroo` to stdout. Once loaded, the shell completes command names, subcommands, and flags.

Global flags (`--token`, `--yes`, `--no-color`) are described in [CLI overview](overview.md#global-flags).

## Synopsis

```bash
muti-metroo completion bash [flags]
muti-metroo completion zsh [flags]
muti-metroo completion fish [flags]
muti-metroo completion powershell [flags]
```

## Flags

All four subcommands take the same flag.

| Flag | Default | Description |
| --- | --- | --- |
| `--no-descriptions` | `false` | Complete names only, without the one-line description of each command and flag. |

## Loading the script

### bash

Requires the `bash-completion` package.

```bash
# Current session
source <(muti-metroo completion bash)

# Every new session, Linux
muti-metroo completion bash > /etc/bash_completion.d/muti-metroo

# Every new session, macOS with Homebrew
muti-metroo completion bash > $(brew --prefix)/etc/bash_completion.d/muti-metroo
```

### zsh

Completion must be enabled in zsh. If it is not, run once:

```bash
echo "autoload -U compinit; compinit" >> ~/.zshrc
```

```bash
# Current session
source <(muti-metroo completion zsh)

# Every new session, Linux
muti-metroo completion zsh > "${fpath[1]}/_muti-metroo"

# Every new session, macOS with Homebrew
muti-metroo completion zsh > $(brew --prefix)/share/zsh/site-functions/_muti-metroo
```

### fish

```shell
# Current session
muti-metroo completion fish | source

# Every new session
muti-metroo completion fish > ~/.config/fish/completions/muti-metroo.fish
```

### PowerShell

```powershell
# Current session
muti-metroo completion powershell | Out-String | Invoke-Expression
```

To load it in every session, add that line to your PowerShell profile.

Start a new shell after installing a script for every session.

## See Also

- [CLI overview](overview.md): every command, grouped as in `muti-metroo --help`
- [Global flags](overview.md#global-flags): flags that every command accepts
- [Installation](../getting-started/installation.md): install the `muti-metroo` binary
