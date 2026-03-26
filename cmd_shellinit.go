package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runShellInit(args []string) {
	shell := detectShell(args)
	script, err := shellInitScript(shell)
	if err != nil {
		fmt.Fprintf(os.Stderr, "c4sh shell-init: %v\n", err)
		osExit(1)
	}
	fmt.Print(script)
}

// shellInitScript returns the shell integration script for the given shell name.
func shellInitScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashScript, nil
	case "zsh":
		return zshScript, nil
	case "powershell", "pwsh":
		return powershellScript, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (use --bash, --zsh, or --powershell)", shell)
	}
}

func detectShell(args []string) string {
	for _, arg := range args {
		switch arg {
		case "--bash":
			return "bash"
		case "--zsh":
			return "zsh"
		case "--powershell", "--pwsh":
			return "powershell"
		}
	}

	sh := os.Getenv("SHELL")
	if sh != "" {
		base := filepath.Base(sh)
		if strings.HasPrefix(base, "bash") {
			return "bash"
		}
		if strings.HasPrefix(base, "zsh") {
			return "zsh"
		}
		return base
	}
	// No SHELL env var — check for PowerShell via PSModulePath
	if os.Getenv("PSModulePath") != "" {
		return "powershell"
	}
	return ""
}

// __c4sh_needs_c4m checks if args reference a c4m (colon syntax, .c4m suffix,
// or extension-free c4m file exists). Used by wrappers to decide whether to
// route to c4sh or pass through to the real command.
//
// The wrappers only intercept when in c4m context OR when args explicitly
// reference a c4m. Otherwise the user's real command runs untouched.
const sharedScript = `
# c4sh shell integration
# eval "$(c4sh shell-init)" to activate

# Helper: returns 0 (true) if any arg looks like a c4m reference.
__c4sh_needs_c4m() {
    for arg in "$@"; do
        case "$arg" in
            -*) continue ;;          # skip flags
            *.c4m|*.c4m:*|*:*) return 0 ;;
        esac
    done
    return 1
}

# cd is always wrapped — it's the context switch.
function cd {
    eval "$(command c4sh cd "$@")"
}

# Wrappers: route to c4sh when in c4m context or args reference a c4m.
# Otherwise pass through to the real command, preserving user's config.
function ls {
    if [ -n "$C4_CONTEXT" ] || __c4sh_needs_c4m "$@"; then
        command c4sh ls "$@"
    else
        command ls "$@"
    fi
}

# Save the user's cat command (alias or binary) before overriding.
__c4sh_real_cat="$(alias cat 2>/dev/null | sed "s/^alias cat='//" | sed "s/'$//" || echo "command cat")"
[ -z "$__c4sh_real_cat" ] && __c4sh_real_cat="command cat"
unalias cat 2>/dev/null

function cat {
    if [ -n "$C4_CONTEXT" ] || __c4sh_needs_c4m "$@"; then
        command c4sh cat "$@"
    else
        eval "$__c4sh_real_cat" '"$@"'
    fi
}

function cp {
    if [ -n "$C4_CONTEXT" ] || __c4sh_needs_c4m "$@"; then
        command c4sh cp "$@"
    else
        command cp "$@"
    fi
}

function mv {
    if [ -n "$C4_CONTEXT" ] || __c4sh_needs_c4m "$@"; then
        command c4sh mv "$@"
    else
        command mv "$@"
    fi
}

function rm {
    if [ -n "$C4_CONTEXT" ] || __c4sh_needs_c4m "$@"; then
        command c4sh rm "$@"
    else
        command rm "$@"
    fi
}

function mkdir {
    if [ -n "$C4_CONTEXT" ] || __c4sh_needs_c4m "$@"; then
        command c4sh mkdir "$@"
    else
        command mkdir "$@"
    fi
}

# pvd: print virtual directory. Always works.
# In c4m context: full resolvable c4m path (e.g., /path/to/project.c4m:src/)
# Outside context: real working directory (same as pwd)
function pvd {
    command c4sh pvd
}

# Prompt helper: outputs c4m context string when active, empty otherwise.
# Add $(__c4sh_context) to your PS1/PROMPT wherever you want it to appear.
#
# Example (bash):
#   PS1='\u@\h \w$(__c4sh_context) $ '
# Example (zsh):
#   PROMPT='%n@%m %~ $(__c4sh_context) %# '
#
__c4sh_context() {
    if [ -n "$C4_CONTEXT" ]; then
        local name
        name=$(basename "$C4_CONTEXT" .c4m)
        local cwd="${C4_CWD:+/$C4_CWD}"
        printf ' c4 %s:%s' "$name" "${cwd:-/}"
    fi
}
`

const bashScript = sharedScript + `
# Tab completion for bash.
__c4sh_complete() {
    local cur="${COMP_WORDS[COMP_CWORD]}"

    if [ -z "$C4_CONTEXT" ] && [ "${COMP_WORDS[0]}" != "cd" ]; then
        return
    fi

    local IFS=$'\n'
    local completions
    completions=($(command c4sh --complete "$cur" 2>/dev/null))
    if [ ${#completions[@]} -gt 0 ]; then
        COMPREPLY=("${completions[@]}")
    fi
}

complete -o default -F __c4sh_complete ls cat cp mv rm mkdir cd
`

const zshScript = sharedScript + `
# Tab completion for zsh.
__c4sh_complete() {
    if [[ -z "$C4_CONTEXT" && "${words[1]}" != "cd" ]]; then
        _default
        return
    fi

    local completions
    completions=("${(@f)$(command c4sh --complete "${words[CURRENT]}" 2>/dev/null)}")
    if (( ${#completions} )); then
        compadd -a completions
    else
        _default
    fi
}

compdef __c4sh_complete ls cat cp mv rm mkdir cd
`

const powershellScript = `
# c4sh PowerShell integration
# Invoke-Expression (c4sh shell-init --powershell)

# Helper: returns $true if any arg looks like a c4m reference.
function Test-C4mArgs {
    param([string[]]$Args)
    foreach ($arg in $Args) {
        if ($arg -match '\.c4m' -or $arg -match ':') { return $true }
    }
    return $false
}

# Save original aliases/cmdlets so we can restore them in wrappers.
if (Get-Alias ls -ErrorAction SilentlyContinue) { Remove-Alias ls -Force -ErrorAction SilentlyContinue }
if (Get-Alias cat -ErrorAction SilentlyContinue) { Remove-Alias cat -Force -ErrorAction SilentlyContinue }
if (Get-Alias cp -ErrorAction SilentlyContinue) { Remove-Alias cp -Force -ErrorAction SilentlyContinue }
if (Get-Alias mv -ErrorAction SilentlyContinue) { Remove-Alias mv -Force -ErrorAction SilentlyContinue }
if (Get-Alias rm -ErrorAction SilentlyContinue) { Remove-Alias rm -Force -ErrorAction SilentlyContinue }
if (Get-Alias mkdir -ErrorAction SilentlyContinue) { Remove-Alias mkdir -Force -ErrorAction SilentlyContinue }

# cd wrapper: parse c4sh output and set env vars.
function cd {
    param([string]$Path)
    if (-not $Path) { $Path = '' }
    $output = & c4sh cd --powershell $Path 2>&1
    foreach ($line in $output) {
        $line = $line.ToString().Trim()
        if ($line -match '^\$env:(\w+)\s*=\s*(.*)$') {
            $varName = $Matches[1]
            $varValue = $Matches[2].Trim('"', "'")
            [Environment]::SetEnvironmentVariable($varName, $varValue, 'Process')
        }
        elseif ($line -match '^Remove-Item Env:\\(\w+)') {
            $varName = $Matches[1]
            [Environment]::SetEnvironmentVariable($varName, $null, 'Process')
        }
        elseif ($line -match '^Set-Location (.+)$') {
            Set-Location $Matches[1]
        }
    }
}

# Wrappers: route to c4sh when in c4m context, otherwise use native cmdlet.
function ls {
    if ($env:C4_CONTEXT -or (Test-C4mArgs $args)) {
        & c4sh ls @args
    } else {
        Get-ChildItem @args
    }
}

function cat {
    if ($env:C4_CONTEXT -or (Test-C4mArgs $args)) {
        & c4sh cat @args
    } else {
        Get-Content @args
    }
}

function cp {
    if ($env:C4_CONTEXT -or (Test-C4mArgs $args)) {
        & c4sh cp @args
    } else {
        Copy-Item @args
    }
}

function mv {
    if ($env:C4_CONTEXT -or (Test-C4mArgs $args)) {
        & c4sh mv @args
    } else {
        Move-Item @args
    }
}

function rm {
    if ($env:C4_CONTEXT -or (Test-C4mArgs $args)) {
        & c4sh rm @args
    } else {
        Remove-Item @args
    }
}

function mkdir {
    if ($env:C4_CONTEXT -or (Test-C4mArgs $args)) {
        & c4sh mkdir @args
    } else {
        New-Item -ItemType Directory @args
    }
}

# Prompt: show c4m context
function prompt {
    $ctx = $env:C4_CONTEXT
    if ($ctx) {
        $name = [System.IO.Path]::GetFileNameWithoutExtension($ctx)
        $cwd = $env:C4_CWD
        if ($cwd) { $cwd = "/$cwd" }
        "c4 ${name}:${cwd} PS $($executionContext.SessionState.Path.CurrentLocation)> "
    } else {
        "PS $($executionContext.SessionState.Path.CurrentLocation)> "
    }
}
`
