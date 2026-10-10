package prompt

import (
	"strings"
	"testing"
)

func TestReadLineDefault(t *testing.T) {
	SetReader(strings.NewReader("\n"))
	got, err := ReadLine("Name", "fallback")
	if err != nil {
		t.Fatalf("ReadLine error: %v", err)
	}
	if got != "fallback" {
		t.Errorf("ReadLine() = %q, want fallback (empty input uses default)", got)
	}
}

func TestReadLineValue(t *testing.T) {
	SetReader(strings.NewReader("  typed  \n"))
	got, err := ReadLine("Name", "fallback")
	if err != nil {
		t.Fatalf("ReadLine error: %v", err)
	}
	if got != "typed" {
		t.Errorf("ReadLine() = %q, want trimmed 'typed'", got)
	}
}

func TestConfirm(t *testing.T) {
	cases := []struct {
		input      string
		defaultYes bool
		want       bool
	}{
		{"\n", true, true},    // empty accepts default
		{"\n", false, false},  // empty accepts default
		{"y\n", false, true},  // explicit yes overrides default
		{"no\n", true, false}, // explicit no overrides default
		{"YES\n", false, true},
	}
	for _, c := range cases {
		SetReader(strings.NewReader(c.input))
		got, err := Confirm("Proceed?", c.defaultYes)
		if err != nil {
			t.Fatalf("Confirm(%q) error: %v", c.input, err)
		}
		if got != c.want {
			t.Errorf("Confirm(input=%q, default=%v) = %v, want %v", c.input, c.defaultYes, got, c.want)
		}
	}
}

func TestSelectDefaultAndChoice(t *testing.T) {
	opts := []string{"a", "b", "c"}

	SetReader(strings.NewReader("\n")) // empty -> default index
	idx, err := Select("Pick", opts, 2)
	if err != nil {
		t.Fatalf("Select error: %v", err)
	}
	if idx != 2 {
		t.Errorf("Select() default = %d, want 2", idx)
	}

	SetReader(strings.NewReader("1\n")) // explicit first option
	idx, err = Select("Pick", opts, 2)
	if err != nil {
		t.Fatalf("Select error: %v", err)
	}
	if idx != 0 {
		t.Errorf("Select() = %d, want 0", idx)
	}
}

func TestSelectRetriesOnInvalid(t *testing.T) {
	// First an out-of-range number, then a valid one.
	SetReader(strings.NewReader("9\n2\n"))
	idx, err := Select("Pick", []string{"a", "b", "c"}, 0)
	if err != nil {
		t.Fatalf("Select error: %v", err)
	}
	if idx != 1 {
		t.Errorf("Select() = %d, want 1 after retry", idx)
	}
}

func TestReadMultiLineTerminatesOnEmptyLine(t *testing.T) {
	SetReader(strings.NewReader("line one\nline two\n\nignored\n"))
	got, err := ReadMultiLine("Paste")
	if err != nil {
		t.Fatalf("ReadMultiLine error: %v", err)
	}
	if got != "line one\nline two" {
		t.Errorf("ReadMultiLine() = %q, want two lines before the blank", got)
	}
}

func TestReadMultiLineTerminatesOnEOF(t *testing.T) {
	// No trailing newline on the final line, and no terminating blank line.
	SetReader(strings.NewReader("only line"))
	got, err := ReadMultiLine("Paste")
	if err != nil {
		t.Fatalf("ReadMultiLine error: %v", err)
	}
	if got != "only line" {
		t.Errorf("ReadMultiLine() = %q, want 'only line' (EOF flush)", got)
	}
}

func TestMultiSelect(t *testing.T) {
	opts := []string{"ingress", "transit", "exit"}

	SetReader(strings.NewReader("1,3\n"))
	got, err := MultiSelect("Roles", opts, []int{1})
	if err != nil {
		t.Fatalf("MultiSelect error: %v", err)
	}
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("MultiSelect() = %v, want [0 2]", got)
	}

	// Empty input keeps the provided selection.
	SetReader(strings.NewReader("\n"))
	got, err = MultiSelect("Roles", opts, []int{1})
	if err != nil {
		t.Fatalf("MultiSelect error: %v", err)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("MultiSelect() = %v, want [1] (default kept)", got)
	}
}
