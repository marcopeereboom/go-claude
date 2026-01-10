package display

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"golang.org/x/term"
)

// display.go - Terminal output formatting and syntax highlighting
//
// CRITICAL: This file ONLY handles terminal display. It NEVER writes files.
// All functions that colorize check IsTTY and return plain text if false.
//
// Separation of concerns:
// - display.go: Format output for humans (terminal)
// - storage.go: Save/load files (always plain text, no ANSI codes)
// - Business logic: Calls display functions, writes files separately

// ANSI color codes for terminal output
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

// IsTTY detects if output is going to a terminal (not a file/pipe)
func IsTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// ShowDiff displays a unified diff between old and new content.
// Adds git-style colors if stderr is a TTY.
// Never modifies the actual content - only display formatting.
func ShowDiff(old, new string) {
	usesColor := IsTTY(os.Stderr)
	diff := generateUnifiedDiff(old, new)

	// Print line by line with optional coloring
	for _, line := range strings.Split(diff, "\n") {
		if usesColor {
			printColoredDiffLine(line)
		} else {
			fmt.Fprintln(os.Stderr, line)
		}
	}
}

// generateUnifiedDiff creates a unified diff between old and new.
// Returns plain text (no ANSI codes) - coloring happens in display layer.
func generateUnifiedDiff(old, new string) string {
	// Handle edge cases
	if old == "" && new == "" {
		return ""
	}
	if old == "" {
		// New file
		lines := strings.Split(strings.TrimRight(new, "\n"), "\n")
		var sb strings.Builder
		sb.WriteString("--- /dev/null\n")
		sb.WriteString("+++ new file\n")
		fmt.Fprintf(&sb, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, line := range lines {
			sb.WriteString("+" + line + "\n")
		}
		return sb.String()
	}
	if new == "" {
		// File deletion
		lines := strings.Split(strings.TrimRight(old, "\n"), "\n")
		var sb strings.Builder
		sb.WriteString("--- old file\n")
		sb.WriteString("+++ /dev/null\n")
		fmt.Fprintf(&sb, "@@ -1,%d +0,0 @@\n", len(lines))
		for _, line := range lines {
			sb.WriteString("-" + line + "\n")
		}
		return sb.String()
	}

	// Both files exist - compute diff
	oldLines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(new, "\n"), "\n")

	return simpleDiff(oldLines, newLines)
}

// simpleDiff creates a basic unified diff (not Myers algorithm, but good enough)
func simpleDiff(oldLines, newLines []string) string {
	var sb strings.Builder
	sb.WriteString("--- old\n")
	sb.WriteString("+++ new\n")

	// Simple line-by-line comparison
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	// Track changes for hunk header
	changeStart := -1
	oldCount := 0
	newCount := 0

	for i := 0; i < maxLen; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			// Start new hunk if needed
			if changeStart == -1 {
				changeStart = i
			}

			// Track what changed
			if oldLine != "" && newLine != "" {
				// Line modified
				sb.WriteString("-" + oldLine + "\n")
				sb.WriteString("+" + newLine + "\n")
				oldCount++
				newCount++
			} else if oldLine != "" {
				// Line deleted
				sb.WriteString("-" + oldLine + "\n")
				oldCount++
			} else {
				// Line added
				sb.WriteString("+" + newLine + "\n")
				newCount++
			}
		}
	}

	if changeStart == -1 {
		return "--- old\n+++ new\n(no changes)\n"
	}

	// Prepend hunk header
	hunkHeader := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
		changeStart+1, oldCount, changeStart+1, newCount)
	return "--- old\n+++ new\n" + hunkHeader + sb.String()[len("--- old\n+++ new\n"):]
}

// printColoredDiffLine prints a single diff line with git-style colors
func printColoredDiffLine(line string) {
	if len(line) == 0 {
		fmt.Fprintln(os.Stderr)
		return
	}

	switch line[0] {
	case '-':
		if strings.HasPrefix(line, "---") {
			// File header
			fmt.Fprintf(os.Stderr, "%s%s%s\n", colorBold, line, colorReset)
		} else {
			// Deletion
			fmt.Fprintf(os.Stderr, "%s%s%s\n", colorRed, line, colorReset)
		}
	case '+':
		if strings.HasPrefix(line, "+++") {
			// File header
			fmt.Fprintf(os.Stderr, "%s%s%s\n", colorBold, line, colorReset)
		} else {
			// Addition
			fmt.Fprintf(os.Stderr, "%s%s%s\n", colorGreen, line, colorReset)
		}
	case '@':
		// Hunk header
		fmt.Fprintf(os.Stderr, "%s%s%s\n", colorCyan, line, colorReset)
	default:
		// Context line
		fmt.Fprintln(os.Stderr, line)
	}
}

// FormatResponse formats Claude's API response for display.
// Uses chroma for syntax highlighting if output is a TTY.
// Never modifies actual content - only display layer.
func FormatResponse(w io.Writer, content string) {
	if !IsTTY(os.Stdout) {
		// Not a TTY - write plain text (e.g., piped to file)
		fmt.Fprint(w, content)
		return
	}

	// Parse and format markdown with syntax highlighting
	formatMarkdownWithChroma(w, content)
}

// formatMarkdownWithChroma applies syntax highlighting to markdown content.
// Uses chroma library to handle all language detection and highlighting.
// NO manual ANSI code injection - chroma handles everything.
func formatMarkdownWithChroma(w io.Writer, content string) {
	lines := strings.Split(content, "\n")
	inCodeBlock := false
	var codeBuffer strings.Builder
	var codeLang string

	for i, line := range lines {
		// Detect code fence markers
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				// End of code block - highlight and flush
				highlightedCode := highlightCode(
					codeBuffer.String(),
					codeLang,
				)
				fmt.Fprint(w, highlightedCode)
				fmt.Fprintf(w, "%s```%s\n", colorGray, colorReset)

				inCodeBlock = false
				codeBuffer.Reset()
				codeLang = ""
			} else {
				// Start of code block
				codeLang = strings.TrimPrefix(line, "```")
				codeLang = strings.TrimSpace(codeLang)
				fmt.Fprintf(w, "%s```%s%s\n",
					colorGray, codeLang, colorReset)
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			// Accumulate code lines
			codeBuffer.WriteString(line)
			if i < len(lines)-1 {
				codeBuffer.WriteString("\n")
			}
		} else {
			// Format regular markdown line
			formatMarkdownLine(w, line)
		}
	}

	// Handle unclosed code block
	if inCodeBlock {
		highlightedCode := highlightCode(codeBuffer.String(), codeLang)
		fmt.Fprint(w, highlightedCode)
	}

	// Ensure trailing newline for clean terminal output
	if !strings.HasSuffix(content, "\n") {
		fmt.Fprintln(w)
	}
}

// highlightCode uses chroma to syntax highlight code.
// Returns plain text if chroma fails or language is unknown.
func highlightCode(code, language string) string {
	if language == "" {
		// No language specified - return as-is
		return colorYellow + code + colorReset + "\n"
	}

	var buf bytes.Buffer
	// Use chroma with terminal256 formatter and monokai style
	err := quick.Highlight(&buf, code, language, "terminal256", "monokai")
	if err != nil {
		// Fallback to plain yellow if highlighting fails
		return colorYellow + code + colorReset + "\n"
	}

	return buf.String()
}

// formatMarkdownLine applies basic formatting to non-code markdown lines
func formatMarkdownLine(w io.Writer, line string) {
	// Headers
	if strings.HasPrefix(line, "#") {
		fmt.Fprintf(w, "%s%s%s%s\n",
			colorBold, colorBlue, line, colorReset)
		return
	}

	// Bullet points
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "-") ||
		strings.HasPrefix(trimmed, "*") ||
		strings.HasPrefix(trimmed, "+") {
		fmt.Fprintf(w, "%s%s%s\n", colorCyan, line, colorReset)
		return
	}

	// Numbered lists
	if len(trimmed) > 0 && trimmed[0] >= '0' && trimmed[0] <= '9' {
		if idx := strings.Index(trimmed, "."); idx > 0 && idx < 4 {
			fmt.Fprintf(w, "%s%s%s\n", colorCyan, line, colorReset)
			return
		}
	}

	// Block quotes
	if strings.HasPrefix(trimmed, ">") {
		fmt.Fprintf(w, "%s%s%s\n", colorGray, line, colorReset)
		return
	}

	// Regular text
	fmt.Fprintln(w, line)
}

// ToolHeader prints a styled tool execution header to stderr
func ToolHeader(name string, dryRun bool) {
	if !IsTTY(os.Stderr) {
		if dryRun {
			fmt.Fprintf(os.Stderr, "\n=== %s (dry-run) ===\n", name)
		} else {
			fmt.Fprintf(os.Stderr, "\n=== %s ===\n", name)
		}
		return
	}

	// Colored output for TTY
	if dryRun {
		fmt.Fprintf(os.Stderr, "\n%s%s=== %s (dry-run) ===%s\n",
			colorBold, colorYellow, name, colorReset)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s%s=== %s ===%s\n",
			colorBold, colorCyan, name, colorReset)
	}
}

// ToolResult prints a styled tool execution result to stderr
func ToolResult(success bool, message string) {
	if !IsTTY(os.Stderr) {
		fmt.Fprintln(os.Stderr, message)
		return
	}

	if success {
		fmt.Fprintf(os.Stderr, "%s✓%s %s\n",
			colorGreen, colorReset, message)
	} else {
		fmt.Fprintf(os.Stderr, "%s✗%s %s\n",
			colorRed, colorReset, message)
	}
}

// Warning prints a warning message to stderr
func Warning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !IsTTY(os.Stderr) {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", msg)
		return
	}

	fmt.Fprintf(os.Stderr, "%s⚠ Warning:%s %s\n",
		colorYellow, colorReset, msg)
}

// Info prints an informational message to stderr
func Info(format string, args ...interface{}) {
	if !IsTTY(os.Stderr) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
		return
	}

	fmt.Fprintf(os.Stderr, "%s%s%s\n",
		colorGray, fmt.Sprintf(format, args...), colorReset)
}
