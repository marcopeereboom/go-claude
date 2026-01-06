package main

import (
	"strings"
	"testing"
)

// TestValidateCommandSimple - minimal test to isolate the issue
func TestValidateCommandSimple(t *testing.T) {
	cmd := "ls && rm file.txt"
	err := validateCommand(cmd)
	
	t.Logf("Command: %q", cmd)
	t.Logf("Error: %v", err)
	
	if err == nil {
		t.Fatal("validateCommand() returned nil, expected error")
	}
	
	errStr := err.Error()
	t.Logf("Error string: %q", errStr)
	
	if !strings.Contains(errStr, "blocked pattern") {
		t.Errorf("error should contain 'blocked pattern', got: %q", errStr)
	}
	
	if !strings.Contains(errStr, "&&") {
		t.Errorf("error should contain '&&', got: %q", errStr)
	}
}

// TestValidateCommandOr - test || blocking
func TestValidateCommandOr(t *testing.T) {
	cmd := "ls || echo fail"
	err := validateCommand(cmd)
	
	t.Logf("Command: %q", cmd)
	t.Logf("Error: %v", err)
	
	if err == nil {
		t.Fatal("validateCommand() returned nil, expected error")
	}
	
	if !strings.Contains(err.Error(), "||") {
		t.Errorf("error should contain '||', got: %q", err.Error())
	}
}

// TestStringsContains - verify strings.Contains works as expected
func TestStringsContains(t *testing.T) {
	tests := []struct {
		s      string
		substr string
		want   bool
	}{
		{"ls && rm", "&&", true},
		{"ls||rm", "||", true},
		{"ls & rm", "&&", false},
		{"ls | rm", "||", false},
	}
	
	for _, tt := range tests {
		got := strings.Contains(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("strings.Contains(%q, %q) = %v, want %v",
				tt.s, tt.substr, got, tt.want)
		}
	}
}
