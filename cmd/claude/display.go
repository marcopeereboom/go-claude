package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"

	colorBold = "\033[1m"
)

// isTTY detects if output is going to a terminal
func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// ShowDiff displays a unified diff between old and new content.
// If stderr is a TTY, uses color output.
func ShowDiff(old, new string) {
	usesColor := isTTY(os.Stderr)
	diff := generateUnifiedDiff(old, new)

	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		line := scanner.Text()
		if usesColor {
			printColoredDiffLine(line)
		} else {
			fmt.Fprintln(os.Stderr, line)
		}
	}
}

// generateUnifiedDiff creates a unified diff between old and new
func generateUnifiedDiff(old, new string) string {
	if old == "" {
		// New file - just show all lines prefixed with +
		var sb strings.Builder
		sb.WriteString("--- /dev/null\n")
		sb.WriteString("+++ new file\n")
		sb.WriteString("@@ -0,0 +")
		lines := strings.Split(strings.TrimRight(new, "\n"), "\n")
		fmt.Fprintf(&sb, "1,%d @@\n", len(lines))
		for _, line := range lines {
			sb.WriteString("+" + line + "\n")
		}
		return sb.String()
	}

	if new == "" {
		// File deletion - show all lines prefixed with -
		var sb strings.Builder
		sb.WriteString("--- old file\n")
		sb.WriteString("+++ /dev/null\n")
		lines := strings.Split(strings.TrimRight(old, "\n"), "\n")
		fmt.Fprintf(&sb, "@@ -1,%d +0,0 @@\n", len(lines))
		for _, line := range lines {
			sb.WriteString("-" + line + "\n")
		}
		return sb.String()
	}

	// Both exist - compute diff
	oldLines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(new, "\n"), "\n")

	hunks := computeDiffHunks(oldLines, newLines)
	if len(hunks) == 0 {
		return "--- old\n+++ new\n(no changes)\n"
	}

	return formatUnifiedDiff(oldLines, newLines, hunks)
}

// diffHunk represents a single change location
type diffHunk struct {
	oldStart, oldLen int
	newStart, newLen int
	lines            []diffLine
}

type diffLine struct {
	kind byte   // ' ', '-', '+'
	text string
}

// computeDiffHunks uses Myers' diff algorithm (simplified)
func computeDiffHunks(oldLines, newLines []string) []diffHunk {
	// Simple line-by-line diff with context
	const contextLines = 3

	// Build edit script
	edits := computeEdits(oldLines, newLines)
	if len(edits) == 0 {
		return nil
	}

	// Group edits into hunks with context
	hunks := []diffHunk{}
	var current *diffHunk

	for i := 0; i < len(oldLines) || i < len(newLines); i++ {
		edit := edits[i]

		// Check if this is a change or context
		isChange := edit != ' '

		// Start new hunk if needed
		if isChange && current == nil {
			start := max(0, i-contextLines)
			current = &diffHunk{
				oldStart: start + 1,
				newStart: start + 1,
			}

			// Add leading context
			for j := start; j < i; j++ {
				current.lines = append(current.lines, diffLine{' ', oldLines[j]})
				current.oldLen++
				current.newLen++
			}
		}

		if current != nil {
			switch edit {
			case ' ': // unchanged
				current.lines = append(current.lines, diffLine{' ', oldLines[i]})
				current.oldLen++
				current.newLen++

				// Check if we should close this hunk
				if i < len(edits)-1 {
					nextChangeIdx := findNextChange(edits, i+1)
					if nextChangeIdx == -1 || nextChangeIdx-i > 2*contextLines {
						// Close hunk
						hunks = append(hunks, *current)
						current = nil
					}
				}

			case '-': // deletion
				if i < len(oldLines) {
					current.lines = append(current.lines,
						diffLine{'-', oldLines[i]})
					current.oldLen++
				}

			case '+': // addition
				if i < len(newLines) {
					current.lines = append(current.lines,
						diffLine{'+', newLines[i]})
					current.newLen++
				}
			}
		}

		if edit == ' ' && i == len(edits)-1 && current != nil {
			// End of file - close hunk
			hunks = append(hunks, *current)
			current = nil
		}
	}

	if current != nil {
		hunks = append(hunks, *current)
	}

	return hunks
}

// computeEdits creates a simple edit script (' ', '-', '+')
func computeEdits(oldLines, newLines []string) []byte {
	maxLen := max(len(oldLines), len(newLines))
	edits := make([]byte, maxLen)

	for i := 0; i < maxLen; i++ {
		if i >= len(oldLines) {
			edits[i] = '+'
		} else if i >= len(newLines) {
			edits[i] = '-'
		} else if oldLines[i] != newLines[i] {
			// Changed line - mark as delete old + add new
			edits[i] = '-'
			// This is simplified; real diff is more sophisticated
		} else {
			edits[i] = ' '
		}
	}

	return edits
}

// findNextChange finds the next non-space edit
func findNextChange(edits []byte, start int) int {
	for i := start; i < len(edits); i++ {
		if edits[i] != ' ' {
			return i
		}
	}
	return -1
}

// formatUnifiedDiff creates the final unified diff output
func formatUnifiedDiff(oldLines, newLines []string, hunks []diffHunk) string {
	var sb strings.Builder
	sb.WriteString("--- old\n")
	sb.WriteString("+++ new\n")

	for _, hunk := range hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n",
			hunk.oldStart, hunk.oldLen,
			hunk.newStart, hunk.newLen)

		for _, line := range hunk.lines {
			sb.WriteByte(line.kind)
			sb.WriteString(line.text)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// printColoredDiffLine prints a single diff line with color
func printColoredDiffLine(line string) {
	if len(line) == 0 {
		fmt.Fprintln(os.Stderr)
		return
	}

	switch line[0] {
	case '-':
		if strings.HasPrefix(line, "---") {
			fmt.Fprintf(os.Stderr, "%s%s%s\n",
				colorBold, line, colorReset)
		} else {
			fmt.Fprintf(os.Stderr, "%s%s%s\n",
				colorRed, line, colorReset)
		}
	case '+':
		if strings.HasPrefix(line, "+++") {
			fmt.Fprintf(os.Stderr, "%s%s%s\n",
				colorBold, line, colorReset)
		} else {
			fmt.Fprintf(os.Stderr, "%s%s%s\n",
				colorGreen, line, colorReset)
		}
	case '@':
		fmt.Fprintf(os.Stderr, "%s%s%s\n",
			colorCyan, line, colorReset)
	default:
		fmt.Fprintln(os.Stderr, line)
	}
}

// FormatMarkdown applies syntax highlighting to markdown content for TTY.
// Does NOT modify content if not a TTY.
func FormatMarkdown(w io.Writer, content string) {
	if !isTTY(os.Stdout) {
		// Not a TTY - just write plain text
		fmt.Fprint(w, content)
		return
	}

	// Apply basic markdown highlighting
	scanner := bufio.NewScanner(strings.NewReader(content))
	inCodeBlock := false
	var codeLang string

	for scanner.Scan() {
		line := scanner.Text()

		// Detect code block markers
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				// End code block
				fmt.Fprintf(w, "%s%s%s\n",
					colorGray, line, colorReset)
				inCodeBlock = false
				codeLang = ""
			} else {
				// Start code block
				codeLang = strings.TrimPrefix(line, "```")
				fmt.Fprintf(w, "%s%s%s\n",
					colorGray, line, colorReset)
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			// Inside code block - use appropriate highlighting
			printCodeLine(w, line, codeLang)
		} else {
			// Regular markdown
			printMarkdownLine(w, line)
		}
	}
}

// printCodeLine applies basic syntax highlighting to code
func printCodeLine(w io.Writer, line, lang string) {
	// Simple keyword highlighting for Go
	if lang == "go" || lang == "golang" {
		line = highlightGoKeywords(line)
	} else if lang == "bash" || lang == "sh" {
		line = highlightBashKeywords(line)
	}

	fmt.Fprintf(w, "%s%s%s\n", colorYellow, line, colorReset)
}

// highlightGoKeywords applies basic Go syntax highlighting
func highlightGoKeywords(line string) string {
	keywords := []string{
		"package", "import", "func", "type", "struct", "interface",
		"const", "var", "if", "else", "for", "range", "return",
		"defer", "go", "select", "case", "switch", "break", "continue",
		"map", "chan", "make", "new", "len", "cap", "append",
	}

	result := line
	for _, kw := range keywords {
		// Simple word boundary replacement
		result = strings.ReplaceAll(result,
			" "+kw+" ", " "+colorBlue+kw+colorReset+colorYellow+" ")
	}
	return result
}

// highlightBashKeywords applies basic bash syntax highlighting
func highlightBashKeywords(line string) string {
	keywords := []string{
		"if", "then", "else", "elif", "fi", "for", "do", "done",
		"while", "case", "esac", "function", "echo", "export",
	}

	result := line
	for _, kw := range keywords {
		result = strings.ReplaceAll(result,
			" "+kw+" ", " "+colorBlue+kw+colorReset+colorYellow+" ")
	}
	return result
}

// printMarkdownLine applies highlighting to regular markdown
func printMarkdownLine(w io.Writer, line string) {
	// Detect headers
	if strings.HasPrefix(line, "#") {
		fmt.Fprintf(w, "%s%s%s\n", colorBold+colorBlue, line, colorReset)
		return
	}

	// Detect bullet points
	if strings.HasPrefix(strings.TrimSpace(line), "-") ||
		strings.HasPrefix(strings.TrimSpace(line), "*") {
		fmt.Fprintf(w, "%s%s%s\n", colorCyan, line, colorReset)
		return
	}

	// Regular text
	fmt.Fprintln(w, line)
}

// ToolHeader prints a styled tool execution header
func ToolHeader(name string, dryRun bool) {
	if !isTTY(os.Stderr) {
		if dryRun {
			fmt.Fprintf(os.Stderr, "\n=== %s (dry-run) ===\n", name)
		} else {
			fmt.Fprintf(os.Stderr, "\n=== %s ===\n", name)
		}
		return
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "\n%s%s=== %s (dry-run) ===%s\n",
			colorBold, colorYellow, name, colorReset)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s%s=== %s ===%s\n",
			colorBold, colorCyan, name, colorReset)
	}
}

// ToolResult prints a styled tool result
func ToolResult(success bool, message string) {
	if !isTTY(os.Stderr) {
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

// Warning prints a warning message
func Warning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !isTTY(os.Stderr) {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", msg)
		return
	}

	fmt.Fprintf(os.Stderr, "%s⚠ Warning:%s %s\n",
		colorYellow, colorReset, msg)
}

// Info prints an informational message (only if verbose)
func Info(format string, args ...interface{}) {
	if !isTTY(os.Stderr) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
		return
	}

	fmt.Fprintf(os.Stderr, "%s%s%s\n",
		colorGray, fmt.Sprintf(format, args...), colorReset)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
