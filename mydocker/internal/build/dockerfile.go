package build

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Instruction represents a single Dockerfile instruction
type Instruction struct {
	Command string   // FROM, RUN, COPY, ENV, CMD, etc.
	Args    []string // parsed arguments
	Raw     string   // original line
}

// Dockerfile holds all parsed instructions
type Dockerfile struct {
	Instructions []Instruction
}

// Parse reads and parses a Dockerfile
func Parse(path string) (*Dockerfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Dockerfile: %w", err)
	}
	defer f.Close()

	df := &Dockerfile{}
	scanner := bufio.NewScanner(f)
	var continuationLine string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle line continuation with backslash
		if strings.HasSuffix(line, "\\") {
			continuationLine += strings.TrimSuffix(line, "\\") + " "
			continue
		}

		if continuationLine != "" {
			line = continuationLine + line
			continuationLine = ""
		}

		instr, err := parseInstruction(line)
		if err != nil {
			continue // skip invalid lines
		}
		df.Instructions = append(df.Instructions, instr)
	}

	return df, scanner.Err()
}

func parseInstruction(line string) (Instruction, error) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return Instruction{}, fmt.Errorf("invalid instruction: %s", line)
	}

	cmd := strings.ToUpper(parts[0])
	rest := strings.TrimSpace(parts[1])

	instr := Instruction{
		Command: cmd,
		Raw:     line,
	}

	switch cmd {
	case "FROM":
		instr.Args = []string{rest}
	case "RUN":
		// Handle both shell form and exec form
		if strings.HasPrefix(rest, "[") {
			instr.Args = parseJSONArray(rest)
		} else {
			instr.Args = []string{"/bin/sh", "-c", rest}
		}
	case "CMD":
		if strings.HasPrefix(rest, "[") {
			instr.Args = parseJSONArray(rest)
		} else {
			instr.Args = []string{"/bin/sh", "-c", rest}
		}
	case "ENTRYPOINT":
		if strings.HasPrefix(rest, "[") {
			instr.Args = parseJSONArray(rest)
		} else {
			instr.Args = []string{"/bin/sh", "-c", rest}
		}
	case "ENV":
		// ENV KEY=VALUE or ENV KEY VALUE
		if strings.Contains(rest, "=") {
			instr.Args = parseEnvEquals(rest)
		} else {
			kv := strings.SplitN(rest, " ", 2)
			if len(kv) == 2 {
				instr.Args = []string{kv[0] + "=" + kv[1]}
			}
		}
	case "COPY", "ADD":
		instr.Args = splitArgs(rest)
	case "WORKDIR":
		instr.Args = []string{rest}
	case "EXPOSE":
		instr.Args = strings.Fields(rest)
	case "LABEL":
		instr.Args = []string{rest}
	case "USER":
		instr.Args = []string{rest}
	case "ARG":
		instr.Args = []string{rest}
	default:
		instr.Args = []string{rest}
	}

	return instr, nil
}

// parseJSONArray parses ["cmd", "arg1", "arg2"]
func parseJSONArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	parts := strings.Split(s, ",")
	result := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseEnvEquals parses KEY=VALUE KEY2=VALUE2
func parseEnvEquals(s string) []string {
	result := []string{}
	// Simple split on spaces, respecting quoted values
	parts := splitArgs(s)
	for _, p := range parts {
		if strings.Contains(p, "=") {
			result = append(result, p)
		}
	}
	return result
}

// splitArgs splits args respecting quoted strings
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
		} else if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}
