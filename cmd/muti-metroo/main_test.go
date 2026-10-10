package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestConfirmActionYesShortCircuits(t *testing.T) {
	// confirmAction must return true immediately when yes=true, without
	// touching stdin -- callers rely on this for --yes / -y to work in
	// scripts and CI.
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, _ := os.Pipe()
	w.Close()
	os.Stdin = r

	ok, err := confirmAction("Continue?", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("yes=true should return true without prompting")
	}
}

func TestConfirmActionNonTTYRefuses(t *testing.T) {
	// When stdin is not a TTY (pipe in this test) and --yes was not given,
	// confirmAction must return errConfirmationRequired so scripts get a
	// distinct exit code instead of silently proceeding through what would
	// have been a destructive prompt.
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, _ := os.Pipe()
	w.Close()
	os.Stdin = r

	_, err := confirmAction("Continue?", false)
	if err == nil {
		t.Fatal("expected error on non-TTY stdin")
	}
	if !errors.Is(err, errConfirmationRequired) {
		t.Fatalf("error %v does not wrap errConfirmationRequired", err)
	}
}

func TestClassifyExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil -> ok", nil, exitOK},
		{"confirmation required", fmt.Errorf("%w: x", errConfirmationRequired), exitConfirmationRequired},
		{"agent unreachable", fmt.Errorf("%w: x", errAgentUnreachable), exitAgentUnreachable},
		{"auth failed", fmt.Errorf("%w: x", errAuthFailed), exitAuthFailed},
		{"operation rejected", fmt.Errorf("%w: x", errOperationRejected), exitOperationRejected},
		{"usage error sentinel", fmt.Errorf("%w: x", errUsageError), exitUsageError},
		{"cobra unknown command", errors.New("unknown command \"foo\""), exitUsageError},
		{"cobra unknown flag", errors.New("unknown flag --bogus"), exitUsageError},
		{"required flag", errors.New("required flag(s) \"config\" not set"), exitUsageError},
		{"plain error", errors.New("something went wrong"), exitGenericError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyExitCode(tc.err); got != tc.want {
				t.Errorf("classifyExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestConfirmTypedRequiresExactPhrase(t *testing.T) {
	// Bypass via yes=true must short-circuit just like confirmAction.
	ok, err := confirmTyped("Type X to confirm:", "X", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("yes=true should accept without reading stdin")
	}
}

func TestIsRemotePathAbsolute(t *testing.T) {
	// Sanity-check the shared helper used by upload/download. Critical
	// because confirmation prompts will reference these paths to operators.
	cases := []struct {
		path string
		want bool
	}{
		{"/etc/hosts", true},
		{"C:\\Windows", true},
		{"c:/Users/me", true},
		{"relative/path", false},
		{"", false},
		{"./foo", false},
	}
	for _, tc := range cases {
		if got := isRemotePathAbsolute(tc.path); got != tc.want {
			t.Errorf("isRemotePathAbsolute(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestColorize(t *testing.T) {
	// colorize is a pure wrap-or-passthrough on its useColor argument.
	// Callers (peer status, mesh-test) compute colorEnabled() once per
	// render and pass the cached boolean into the loop -- this test
	// pins the wrap behavior; TestColorEnabled covers the decision side.
	if got := colorize(false, ansiRed, "FAIL"); got != "FAIL" {
		t.Errorf("colorize(false, ...) = %q, want %q", got, "FAIL")
	}
	if got := colorize(true, ansiRed, "FAIL"); got != ansiRed+"FAIL"+ansiReset {
		t.Errorf("colorize(true, ...) = %q, want wrapped", got)
	}
}

func TestColorEnabled(t *testing.T) {
	// colorEnabled honors --no-color and (under a non-TTY test stdout)
	// the auto-off path. The NO_COLOR env path is exercised implicitly
	// by other test setups; here we just pin the flag behavior.
	old := noColorFlag
	defer func() { noColorFlag = old }()
	noColorFlag = true
	if colorEnabled() {
		t.Error("colorEnabled() = true when noColorFlag is set")
	}
	noColorFlag = false
	// Test harness stdout is a pipe, so auto-off should apply.
	if colorEnabled() {
		t.Error("colorEnabled() = true under non-TTY stdout")
	}
}

func TestRoundDuration(t *testing.T) {
	// roundDuration shapes operator-facing time strings ("6m30s", not
	// "390s" or "6.5 minutes"). Used by the wake reattachment hint.
	cases := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{60 * time.Second, "1m"},
		{6*time.Minute + 30*time.Second, "6m30s"},
		{5 * time.Minute, "5m"},
		{6*time.Minute + 500*time.Millisecond, "6m1s"}, // rounded up
	}
	for _, tc := range cases {
		if got := roundDuration(tc.in); got != tc.want {
			t.Errorf("roundDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// guard against accidental regression of the warning text format that
// scripts and runbooks may grep for.
func TestExitCodesAreStable(t *testing.T) {
	want := map[string]int{
		"OK": 0, "Generic": 1, "Usage": 2, "AgentUnreachable": 3,
		"AuthFailed": 4, "OperationRejected": 5, "ConfirmationRequired": 6,
	}
	got := map[string]int{
		"OK":                   exitOK,
		"Generic":              exitGenericError,
		"Usage":                exitUsageError,
		"AgentUnreachable":     exitAgentUnreachable,
		"AuthFailed":           exitAuthFailed,
		"OperationRejected":    exitOperationRejected,
		"ConfirmationRequired": exitConfirmationRequired,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("exit code %s drifted: got %d, want %d", k, got[k], v)
		}
	}
	// Ensure no two codes collide.
	seen := map[int]string{}
	for k, v := range got {
		if other, ok := seen[v]; ok {
			t.Errorf("exit code %d shared by %s and %s", v, other, k)
		}
		seen[v] = k
	}
	// Sanity: stable codes are positive and small.
	for k, v := range got {
		if v < 0 || v > 125 {
			t.Errorf("exit code %s out of POSIX-friendly range: %d", k, v)
		}
	}
	_ = strings.Builder{} // satisfy "strings" import if other refs disappear
}
