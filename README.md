# c4sh — Edit Filesystems as Text

[![CI](https://github.com/Avalanche-io/c4sh/actions/workflows/ci.yml/badge.svg)](https://github.com/Avalanche-io/c4sh/actions/workflows/ci.yml)
[![Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](./LICENSE)

In Unix, everything is a file. In c4sh, every filesystem is a file too —
a c4m file. `cd` into it, `ls` its entries, `mv` things around. You're
editing a plain text description of a filesystem with the same commands
you already know. Operations within a c4m don't touch the disk, regardless
of how much data the entries describe.

```bash
$ cd project.c4m
$ ls
shots/  assets/  deliveries/  README.md

$ mv shots/010/ archive/              # edits the c4m — no content moves
$ rm -rf temp/                        # removes entries from the c4m
$ cp shots/ delivery.c4m:shots/       # copies entries between c4m files
$ cd
```

## Install

### Homebrew (recommended — includes c4 and c4sh)

```bash
brew install mrjoshuak/tap/c4
```

### From source

```bash
go install github.com/Avalanche-io/c4sh@latest
```

### Binary downloads

See [c4toolkit](https://github.com/Avalanche-io/c4toolkit) for
pre-built archives and the full suite.

## Shell integration

c4sh wraps `cd`, `ls`, `cat`, `cp`, `mv`, `rm`, and `mkdir` so they work
transparently inside c4m files. Outside c4m context, every command passes
through to the real binary untouched.

Add this line to your shell config:

```bash
# bash (~/.bashrc) or zsh (~/.zshrc)
eval "$(c4sh shell-init)"
```

```powershell
# PowerShell ($PROFILE)
Invoke-Expression (c4sh shell-init --powershell)
```

### Placement matters

The `eval` line must come **after** any `cat` alias. c4sh captures an
existing `cat` alias (like `cat` aliased to `bat`), in bash and in zsh,
and uses it when you're not in a c4m. If the alias is defined after the
eval, it replaces c4sh's `cat`, which then no longer reads c4m entries.

```bash
# ~/.bashrc — correct order

# Your aliases first
alias cat="bat --paging=never"
alias ll="ls -lah"

# c4sh last — captures aliases above
eval "$(c4sh shell-init)"
```

### Prompt

c4sh provides `__c4sh_context`, a function that outputs the current c4m
context when active and nothing otherwise. Add it to your prompt wherever
you want — c4sh does not modify your prompt automatically.

```bash
# bash — add before your prompt character
PS1='\u@\h \w$(__c4sh_context) $ '

# Produces:
#   joshua@host ~/projects $              (normal)
#   joshua@host ~/projects c4 myfile:/ $  (in c4m root)
#   joshua@host ~/projects c4 myfile:/src/ $  (in c4m subdir)
```

```zsh
# zsh (needs prompt_subst)
setopt prompt_subst
PROMPT='%n@%m %~$(__c4sh_context) %# '
```

The context string includes the c4m name (without `.c4m` extension) and
the current path within it. It appears only when you're inside a c4m.

### Virtual directory

`pvd` (print virtual directory) works everywhere:

```bash
~/projects $ pvd
/Users/joshua/projects           # outside c4m: same as pwd

~/projects $ cd project.c4m
~/projects c4 project:/ $ pvd
/Users/joshua/projects/project.c4m:/    # inside c4m: full resolvable path

~/projects c4 project:/ $ cd src
~/projects c4 project:/src/ $ pvd
/Users/joshua/projects/project.c4m:/src/
```

The output is a valid c4m colon path that other c4sh commands understand.
`pwd` is never modified — it always returns the real filesystem path.

### What gets wrapped

| Command | In c4m context | Outside context |
|---------|---------------|-----------------|
| `cd` | Navigate within c4m; `cd ..` from root exits | Normal `cd` |
| `ls` | List c4m entries; `-li` shows C4 IDs | Your normal `ls` (with aliases) |
| `cat` | Stream content from store by C4 ID | Your normal `cat` (preserves bat alias) |
| `cp` | Copy across the c4m boundary | Normal `cp` |
| `mv` | Rename c4m entries | Normal `mv` |
| `rm` | Remove c4m entries | Normal `rm` |
| `mkdir` | Create directory entries | Normal `mkdir` |
| `pvd` | Full c4m colon path | Same as `pwd` |

### ls flags in c4m context

| Flag | Effect |
|------|--------|
| `-l` | Long format: mode, size, date, name |
| `-li` | Long format with C4 IDs (right-aligned on terminal, canonical c4m when piped) |
| `-a` | Include hidden entries |
| `-1` | One entry per line |

`ls -li | grep` outputs canonical c4m entry format — parseable, greppable,
and usable as input to other c4 tools.

## The colon boundary

The `:` in a path separates the real filesystem from c4m space — like a
mount point. Content crosses the boundary via `cp`:

```bash
# Capture: real → c4m
# Walks the tree, computes C4 IDs, stores content, writes c4m entries.
cp ./project/ project.c4m:

# Extract: c4m → real
# Pulls content from the store by C4 ID, writes files to disk.
cp project.c4m:shots/010/ ./workspace/

# Rearrange: c4m → c4m
# Copies c4m entries. No content I/O — same IDs, same store.
cp project.c4m:shots/ delivery.c4m:shots/
```

## Multi-destination copy

Read once, write many — the same principle as `tee`, applied to file copies.
C4 IDs are computed as part of the read, not as a separate pass.

```bash
cp /mnt/card/ today.c4m: /mnt/shuttle/ /mnt/backup/
```

One read of the source produces two real copies and a c4m file with
cryptographic verification for every file. This replaces the typical on-set
workflow of copying to each destination separately and then running a
checksum pass.

## Navigate and edit

Once you're inside a c4m, familiar commands work on its entries.
You can also enter a c4m without the extension — `cd project` works
when `project.c4m` exists.

```bash
cd project.c4m                   # enter the c4m
ls                               # names, directories marked with /
ls -l shots/010/                 # mode, size, date, name
cat shots/010/comp.nk            # streams content from the store
mkdir deliveries/v4/
mv shots/010/comp.nk deliveries/v4/
rm -rf scratch/                  # removes entries; content stays in store
cd                               # back to the real filesystem
```

Outside c4m context, your real commands run untouched — the shell
wrappers pass through to the original `ls`, `cp`, etc.

`rm` removes entries from the c4m text, not content from the store. If
you version the c4m with `c4 diff` and `c4 log`, any state is recoverable.
Without version history, removed entries are lost — but the stored content
is still retrievable by C4 ID.

## Distribute

Bundle a c4m with the store objects it references:

```bash
c4sh pool delivery.c4m ./bundle/
```

The bundle is self-contained: the c4m, a store with only the referenced
objects, and an `extract.sh` that writes its files and folders with
standard Unix tools — no c4 installation required on the receiving end.
It restores content only: files get the store's permissions and the
current time, a symbolic link is written as a copy of its target, and a
link to a folder fails. For modes, times and links, `c4sh ingest` the
bundle and then `c4sh cp delivery.c4m: <dir>/`.

Absorb a received bundle into your local store:

```bash
c4sh ingest ./bundle/
```

`ingest` copies store objects into your local store and copies any c4m
files from the bundle into the current directory, making them immediately
usable.

## Sync over ssh

```bash
c4sh rsync delivery.c4m remote:/deliveries/
c4sh rsync remote:/deliveries/project.c4m .
```

Uses rsync internally. A push sends the c4m and the store objects it
names: each file's content, and each listing and range ID list it
references, when your store holds them. Nothing else in your store is
sent. A pull fetches the c4m, then the objects it names. Objects already
present on the receiving side are skipped, and the counts printed are what
was sent or fetched. A file whose content is missing on the sending side
is named, and the command exits 1 after sending the rest.

The remote store is `.c4/store` under the remote home (set
`C4_REMOTE_STORE` for another path). Each object lands where that store
looks for it, flat or split; a remote with no store yet gets a new one.

## Commands

| Command | What it does | Needs store? |
|---------|-------------|:---:|
| `cd` | Enter or navigate within a c4m file | no |
| `ls` | List c4m entries (`-l` long, `-a` hidden, `-1` one-per-line) | no |
| `cat` | Stream content from store by C4 ID | yes |
| `cp` | Copy across the boundary; multi-destination supported | yes* |
| `mv` | Move or rename entries | no |
| `rm` | Remove entries (`-rf` for directories) | no |
| `mkdir` | Create directory entries (`-p` for parents) | no |
| `pool` | Bundle c4m + store objects for transport | yes |
| `ingest` | Absorb a bundle into local store | yes |
| `rsync` | Sync a c4m and the content it names over ssh | yes |
| `shell-init` | Output shell integration script | no |
| `version` | Print c4sh version | no |

\* `cp` between two c4m files (c4m-to-c4m) does not need the store.

## How it works

A c4m file is one entry per line — mode, timestamp, size, name,
C4 ID — so `grep`, `awk`, `sort`, and `diff` all work on it natively.
c4sh adds filesystem semantics on top: `cd` sets context, `ls` reads
entries, `mv` rewrites names and depths. It's a text editor that speaks
shell.

When piped, `ls` outputs one entry per line (matching real `ls` behavior).

Content lives in a shared store, the one named by `C4_STORE` or by the
`store` line in `~/.c4/config`, the same store the
[c4](https://github.com/Avalanche-io/c4) CLI uses. Store once, access
from either tool.

## Known limitations

- Filenames containing `:` are interpreted as c4m references.
- Prompt integration is best-effort — custom prompts may not display the
  c4m context inline.
- `rsync` requires a local filesystem store (not S3).
- Commands inside c4m context silently ignore unrecognized flags.
- `mv` and `rm` re-derive the folders above an edit, but a link to a
  folder keeps the C4 ID it had when it was captured.

## Design Decisions

See the [FAQ](https://github.com/Avalanche-io/c4/blob/master/docs/faq.md) for design decisions including SHA-512 permanence, the c4m format, and content store scaling.

## Requirements

- the [c4](https://github.com/Avalanche-io/c4) CLI is optional: c4sh
  includes the library; you want `c4` for `c4 diff`, `c4 log` and
  `c4 patch`
- bash, zsh, or PowerShell/pwsh
- rsync (for `c4sh rsync`)

## License

Apache 2.0
