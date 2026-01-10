package main

import (
	"strings"
	"testing"
)

func TestGetVersionInfo(t *testing.T) {
	// Test default values
	oldVersion, oldCommit, oldBuildTime := Version, Commit, BuildTime
	Version = "dev"
	Commit = "unknown"
	BuildTime = "unknown"

	version := GetVersionInfo()
	if !strings.HasPrefix(version, "go-claude dev") {
		t.Errorf("Expected version to start with 'go-claude dev', got: %s", version)
	}
	if !strings.Contains(version, "(go ") {
		t.Errorf("Expected version to contain Go version info, got: %s", version)
	}

	// Test with commit hash
	Commit = "abcdef1234567890"
	version = GetVersionInfo()
	if !strings.Contains(version, "(abcdef1)") {
		t.Errorf("Expected version to contain short commit hash (abcdef1), got: %s", version)
	}

	// Test with build time
	BuildTime = "2026-01-10T12:00:00Z"
	version = GetVersionInfo()
	if !strings.Contains(version, "built 2026-01-10T12:00:00Z") {
		t.Errorf("Expected version to contain build time, got: %s", version)
	}

	// Test with real version
	Version = "v1.0.0"
	version = GetVersionInfo()
	if !strings.HasPrefix(version, "go-claude v1.0.0") {
		t.Errorf("Expected version to start with 'go-claude v1.0.0', got: %s", version)
	}

	// Restore original values
	Version, Commit, BuildTime = oldVersion, oldCommit, oldBuildTime
}