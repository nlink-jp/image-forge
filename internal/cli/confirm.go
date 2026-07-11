package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// confirmFunc gates a destructive action (deleting weight files). It receives a
// human-readable summary of exactly what will be deleted and returns whether to
// proceed. Production wires stdinConfirm (an interactive-terminal prompt); tests
// inject a stub so a real terminal is never required — and never touched.
//
// The whole point of routing every destructive delete through this type is that
// a non-interactive context (a script, a pipe, a test run) can NEVER confirm, so
// it can never delete. A `models gc --force` accidentally invoked with the wrong
// models directory — the exact accident that motivated this — deletes nothing
// unless a human types "yes" at a terminal.
type confirmFunc func(summary string) bool

// stdinConfirm is the production confirmer: it prompts on stdout and reads the
// answer from stdin, requiring an interactive terminal and an explicit "yes".
func stdinConfirm(summary string) bool {
	return confirmDestructive(os.Stdout, os.Stdin, summary)
}

// confirmDestructive prints summary + a prompt to out and reads one line from in,
// returning true ONLY when in is an interactive terminal AND the reply is "yes".
// A non-TTY in (script / pipe / test / cron) is refused without prompting, so an
// unattended destructive delete is impossible by construction.
func confirmDestructive(out io.Writer, in *os.File, summary string) bool {
	if !isInteractive(in) {
		fmt.Fprintln(out, summary)
		fmt.Fprintln(out, "Aborted: deleting files requires typing 'yes' at an interactive terminal (no TTY detected). Nothing was deleted.")
		return false
	}
	fmt.Fprintln(out, summary)
	fmt.Fprint(out, "Type 'yes' to permanently delete these files: ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	return affirmative(line)
}

// isInteractive reports whether f is a character device — an interactive
// terminal. Dependency-free (no golang.org/x/term): a pipe, a regular file, or
// /dev/null (how `go test` wires stdin) is not a character device, so this is
// false there. That is the safety property tests rely on.
func isInteractive(f *os.File) bool {
	if f == nil {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// affirmative reports whether a typed reply confirms a destructive action: an
// exact, case-insensitive "yes" and nothing else (not "y", not "yes please").
// Requiring the full word makes an accidental confirmation unlikely. Pure.
func affirmative(input string) bool {
	return strings.EqualFold(strings.TrimSpace(input), "yes")
}
