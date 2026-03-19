package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runShellInit(args []string) {
	shell := detectShell(args)

	switch shell {
	case "bash":
		fmt.Print(bashScript)
	case "zsh":
		fmt.Print(zshScript)
	default:
		fmt.Fprintf(os.Stderr, "c4sh shell-init: unsupported shell %q (use --bash or --zsh)\n", shell)
		os.Exit(1)
	}
}

func detectShell(args []string) string {
	for _, arg := range args {
		switch arg {
		case "--bash":
			return "bash"
		case "--zsh":
			return "zsh"
		}
	}

	sh := os.Getenv("SHELL")
	base := filepath.Base(sh)
	if strings.HasPrefix(base, "bash") {
		return "bash"
	}
	if strings.HasPrefix(base, "zsh") {
		return "zsh"
	}
	return base
}

// Shared shell functions used by both bash and zsh.
// Uses "function name" syntax (not "name()") to avoid alias expansion.
const sharedScript = `
# c4sh shell integration
# eval "$(c4sh shell-init)" to activate

# cd wrapper: delegate to c4sh cd which outputs shell commands to eval.
function cd {
    eval "$(command c4sh cd "$@")"
}

# Shell command wrappers — transparent c4m handling with fallthrough.
function ls    { command c4sh ls "$@"; }
function cat   { command c4sh cat "$@"; }
function cp    { command c4sh cp "$@"; }
function mv    { command c4sh mv "$@"; }
function rm    { command c4sh rm "$@"; }
function mkdir { command c4sh mkdir "$@"; }
`

const bashScript = sharedScript + `
# Prompt: show c4m context inline
__c4sh_original_ps1="${__c4sh_original_ps1:-$PS1}"

__c4sh_prompt() {
    if [ -n "$C4_CONTEXT" ]; then
        local name
        name=$(basename "$C4_CONTEXT" .c4m)
        local cwd="${C4_CWD:+/$C4_CWD}"
        # Insert c4 context before the final $ or > in the prompt
        PS1="${__c4sh_original_ps1/\\$ / c4 ${name}:${cwd} \\$ }"
        # Fallback if substitution didn't match
        if [ "$PS1" = "$__c4sh_original_ps1" ]; then
            PS1="c4 ${name}:${cwd} ${__c4sh_original_ps1}"
        fi
    else
        PS1="$__c4sh_original_ps1"
    fi
}

if [[ ! "${PROMPT_COMMAND:-}" =~ __c4sh_prompt ]]; then
    PROMPT_COMMAND="__c4sh_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi

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
# Prompt: show c4m context inline
__c4sh_original_ps1="${__c4sh_original_ps1:-$PROMPT}"

__c4sh_prompt() {
    if [ -n "$C4_CONTEXT" ]; then
        local name
        name=$(basename "$C4_CONTEXT" .c4m)
        local cwd="${C4_CWD:+/$C4_CWD}"
        PROMPT="${__c4sh_original_ps1/\%\# / c4 ${name}:${cwd} %# }"
        if [ "$PROMPT" = "$__c4sh_original_ps1" ]; then
            PROMPT="c4 ${name}:${cwd} ${__c4sh_original_ps1}"
        fi
    else
        PROMPT="$__c4sh_original_ps1"
    fi
}

autoload -Uz add-zsh-hook
add-zsh-hook precmd __c4sh_prompt

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
