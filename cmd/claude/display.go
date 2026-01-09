package main

import (
	"os"

	"github.com/marcopeereboom/go-claude/pkg/display"
	"golang.org/x/term"
)

// Re-export display functions for backward compatibility
var (
	ShowDiff       = display.ShowDiff
	FormatResponse = display.FormatResponse
	ToolHeader     = display.ToolHeader
	ToolResult     = display.ToolResult
	Warning        = display.Warning
	Info           = display.Info
)

// isTTY detects if output is going to a terminal (not a file/pipe)
func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}
