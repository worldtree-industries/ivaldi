package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Prompter handles interactive user input.
type Prompter struct {
	in      io.Reader
	out     io.Writer
	scanner *bufio.Scanner
}

// New creates a new Prompter with the given input and output streams.
func New(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{
		in:      in,
		out:     out,
		scanner: bufio.NewScanner(in),
	}
}

// String prompts for a string with a default value.
func (p *Prompter) String(message, defaultValue string) string {
	if defaultValue != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", message, defaultValue)
	} else {
		fmt.Fprintf(p.out, "%s: ", message)
	}

	if !p.scanner.Scan() {
		return defaultValue
	}

	input := strings.TrimSpace(p.scanner.Text())
	if input == "" {
		return defaultValue
	}
	return input
}

// ErrNoInput is returned when input ends before a required answer is given.
var ErrNoInput = errors.New("input ended before an answer was given")

// Required prompts for a non-empty string with a default value (none if
// empty), re-asking while the answer is blank.
func (p *Prompter) Required(message, defaultValue string) (string, error) {
	for {
		if defaultValue != "" {
			fmt.Fprintf(p.out, "%s [%s]: ", message, defaultValue)
		} else {
			fmt.Fprintf(p.out, "%s: ", message)
		}

		input, err := p.requiredLine(message)
		if err != nil {
			return "", err
		}
		if input != "" {
			return input, nil
		}
		if defaultValue != "" {
			return defaultValue, nil
		}
		fmt.Fprintln(p.out, "An answer is required.")
	}
}

// requiredLine reads the trimmed answer to message, or returns an error
// wrapping ErrNoInput if input ends first.
func (p *Prompter) requiredLine(message string) (string, error) {
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%s: %w", message, ErrNoInput)
	}
	return strings.TrimSpace(p.scanner.Text()), nil
}

// Bool prompts for a yes/no answer.
func (p *Prompter) Bool(message string, defaultValue bool) bool {
	defaultStr := "y/N"
	if defaultValue {
		defaultStr = "Y/n"
	}

	fmt.Fprintf(p.out, "%s [%s]: ", message, defaultStr)

	if !p.scanner.Scan() {
		return defaultValue
	}

	input := strings.ToLower(strings.TrimSpace(p.scanner.Text()))
	if input == "" {
		return defaultValue
	}

	return input == "y" || input == "yes"
}

// Select prompts the user to select an option from a list.
func (p *Prompter) Select(message string, options []string) int {
	fmt.Fprintln(p.out, message)
	for i, opt := range options {
		fmt.Fprintf(p.out, "%d. %s\n", i+1, opt)
	}

	for {
		fmt.Fprint(p.out, "Select an option: ")
		if !p.scanner.Scan() {
			return 0
		}

		input := strings.TrimSpace(p.scanner.Text())
		choice, err := strconv.Atoi(input)
		if err == nil && choice >= 1 && choice <= len(options) {
			return choice - 1
		}
		fmt.Fprintln(p.out, "Invalid selection. Please try again.")
	}
}

// Choice prompts for one of options, re-asking while the answer isn't one of
// them. It returns an error wrapping ErrNoInput if input ends first.
func (p *Prompter) Choice(message string, options []string) (string, error) {
	for {
		fmt.Fprintf(p.out, "%s (%s): ", message, strings.Join(options, ", "))

		input, err := p.requiredLine(message)
		if err != nil {
			return "", err
		}
		if slices.Contains(options, input) {
			return input, nil
		}
		fmt.Fprintf(p.out, "Choose one of: %s.\n", strings.Join(options, ", "))
	}
}

// Validated prompts like Required, then re-asks, printing the reason, while
// check rejects the answer. It returns an error wrapping ErrNoInput at EOF.
func (p *Prompter) Validated(message, defaultValue string, check func(string) error) (string, error) {
	for {
		input, err := p.Required(message, defaultValue)
		if err != nil {
			return "", err
		}
		if err = check(input); err == nil {
			return input, nil
		}
		fmt.Fprintf(p.out, "Invalid answer %q: %v.\n", input, err)
	}
}
