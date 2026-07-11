package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestAffirmative(t *testing.T) {
	yes := []string{"yes", "YES", "Yes", "  yes  ", "yes\n"}
	no := []string{"y", "no", "", "yeah", "yes please", "ok", "1"}
	for _, s := range yes {
		if !affirmative(s) {
			t.Errorf("affirmative(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if affirmative(s) {
			t.Errorf("affirmative(%q) = true, want false", s)
		}
	}
}

func TestIsInteractive_PipeIsNotTTY(t *testing.T) {
	// os.Pipe read end is not a character device — like `go test`'s stdin.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isInteractive(r) {
		t.Error("a pipe should not be reported as an interactive terminal")
	}
	if isInteractive(nil) {
		t.Error("nil file should not be interactive")
	}
}

// confirmDestructive must refuse (return false) and delete nothing when the input
// is not a terminal — even if a "yes" is waiting on the pipe. This is the exact
// guarantee that makes destructive deletes impossible from scripts and tests.
func TestConfirmDestructive_NonTTYRefusesEvenWithYes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := w.WriteString("yes\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	var out bytes.Buffer
	if confirmDestructive(&out, r, "about to delete everything") {
		t.Fatal("confirmDestructive returned true for a non-TTY input")
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected an abort message, got: %q", out.String())
	}
}
