// Package prompt provides simple terminal prompts for the setup wizard.
// It uses only stdlib and golang.org/x/term to minimize binary size.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

var (
	reader    = bufio.NewReader(os.Stdin)
	lineWidth = 80
)

// SetReader replaces the input source used by every prompt function. Production
// code reads from os.Stdin (the package default); tests use this to feed
// scripted input so the wizard's interactive steps can be exercised. Note that
// ReadPassword still requires stdin not to be a terminal to honor this reader.
func SetReader(r io.Reader) {
	reader = bufio.NewReader(r)
}

// ReadLine reads a single line with optional default value.
func ReadLine(prompt string, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// ReadLineValidated reads with validation function, retrying on failure.
func ReadLineValidated(prompt string, defaultVal string, validate func(string) error) (string, error) {
	for {
		value, err := ReadLine(prompt, defaultVal)
		if err != nil {
			return "", err
		}

		if validate != nil {
			if err := validate(value); err != nil {
				fmt.Printf("  %s: %v\n", "Error", err)
				continue
			}
		}
		return value, nil
	}
}

// ReadMultiLine reads multiple lines until an empty line is entered.
func ReadMultiLine(prompt string) (string, error) {
	fmt.Printf("%s %s\n", prompt, "(enter empty line to finish):")

	// Read from the shared reader (not a fresh os.Stdin scanner) so input can be
	// injected in tests and so it shares the buffer with the other prompts.
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if err != nil {
			if err == io.EOF {
				if trimmed != "" {
					lines = append(lines, trimmed)
				}
				break
			}
			return "", err
		}
		if trimmed == "" {
			break
		}
		lines = append(lines, trimmed)
	}

	return strings.Join(lines, "\n"), nil
}

// ReadPassword reads password without echo using terminal raw mode.
func ReadPassword(prompt string) (string, error) {
	fmt.Printf("%s: ", prompt)

	// Check if stdin is a terminal
	fd := int(syscall.Stdin)
	if !term.IsTerminal(fd) {
		// Fall back to regular input if not a terminal
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	password, err := term.ReadPassword(fd)
	fmt.Println() // Add newline after password input
	if err != nil {
		return "", err
	}

	return string(password), nil
}

// Confirm asks a yes/no question.
func Confirm(prompt string, defaultYes bool) (bool, error) {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}

	for {
		fmt.Printf("%s [%s]: ", prompt, hint)

		line, err := reader.ReadString('\n')
		if err != nil {
			return false, err
		}

		line = strings.TrimSpace(strings.ToLower(line))

		if line == "" {
			return defaultYes, nil
		}

		switch line {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Println("  " + "Please enter 'y' or 'n'")
		}
	}
}

// Select shows a numbered menu and returns the selected index.
func Select(prompt string, options []string, defaultIdx int) (int, error) {
	fmt.Println()
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = "> "
		}
		fmt.Printf("%s%d. %s\n", marker, i+1, opt)
	}
	fmt.Println()

	defaultStr := ""
	if defaultIdx >= 0 && defaultIdx < len(options) {
		defaultStr = strconv.Itoa(defaultIdx + 1)
	}

	for {
		input, err := ReadLine(prompt, defaultStr)
		if err != nil {
			return 0, err
		}

		num, err := strconv.Atoi(input)
		if err != nil || num < 1 || num > len(options) {
			fmt.Printf("  Please enter a number between 1 and %d\n", len(options))
			continue
		}

		return num - 1, nil
	}
}

// MultiSelect shows a numbered menu and returns selected indices.
// Users enter comma-separated numbers (e.g., "1,3,4").
func MultiSelect(prompt string, options []string, selected []int) ([]int, error) {
	fmt.Println()

	// Create a set for quick lookup
	selectedSet := make(map[int]bool)
	for _, idx := range selected {
		selectedSet[idx] = true
	}

	for i, opt := range options {
		marker := "[ ]"
		if selectedSet[i] {
			marker = "[x]"
		}
		fmt.Printf("  %d. %s %s\n", i+1, marker, opt)
	}
	fmt.Println()

	// Build default string from selected indices
	var defaultParts []string
	for _, idx := range selected {
		defaultParts = append(defaultParts, strconv.Itoa(idx+1))
	}
	defaultStr := strings.Join(defaultParts, ",")

	for {
		input, err := ReadLine(prompt+" "+"(comma-separated)", defaultStr)
		if err != nil {
			return nil, err
		}

		if input == "" {
			return selected, nil
		}

		parts := strings.Split(input, ",")
		var result []int
		valid := true

		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			num, err := strconv.Atoi(part)
			if err != nil || num < 1 || num > len(options) {
				fmt.Printf("  Invalid selection: %s (must be 1-%d)\n", part, len(options))
				valid = false
				break
			}
			result = append(result, num-1)
		}

		if valid {
			return result, nil
		}
	}
}

// PrintHeader prints a section header with title and description.
func PrintHeader(title, description string) {
	fmt.Println()
	PrintDivider()
	fmt.Println(title)
	PrintDivider()
	if description != "" {
		fmt.Println(description)
		fmt.Println()
	}
}

// PrintDivider prints a horizontal line.
func PrintDivider() {
	fmt.Println(strings.Repeat("-", lineWidth))
}

// PrintBanner prints the application banner.
func PrintBanner(title, subtitle string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", lineWidth))
	// Center the title
	padding := (lineWidth - len(title)) / 2
	if padding > 0 {
		fmt.Printf("%s%s\n", strings.Repeat(" ", padding), title)
	} else {
		fmt.Println(title)
	}
	fmt.Println(strings.Repeat("=", lineWidth))
	if subtitle != "" {
		subPadding := (lineWidth - len(subtitle)) / 2
		if subPadding > 0 {
			fmt.Printf("%s%s\n", strings.Repeat(" ", subPadding), subtitle)
		} else {
			fmt.Println(subtitle)
		}
	}
	fmt.Println()
}

// PrintNote prints a note or informational message.
func PrintNote(message string) {
	fmt.Println()
	fmt.Println(message)
	fmt.Println()
}

// PrintSuccess prints a success message.
func PrintSuccess(message string) {
	fmt.Printf("[OK] %s\n", message)
}

// PrintError prints an error message.
func PrintError(message string) {
	fmt.Printf("[ERROR] %s\n", message)
}

// PrintWarning prints a warning message.
func PrintWarning(message string) {
	fmt.Printf("[WARNING] %s\n", message)
}

// PrintInfo prints an informational message.
func PrintInfo(message string) {
	fmt.Printf("[INFO] %s\n", message)
}
