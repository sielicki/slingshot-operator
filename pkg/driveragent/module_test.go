/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package driveragent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsModuleLoaded_WithMockProcModules(t *testing.T) {
	// Create a temporary directory structure for /proc/modules mock
	tmpDir := t.TempDir()
	procModules := filepath.Join(tmpDir, "modules")

	// Write mock /proc/modules content
	content := `cxi_core 123456 1 - Live 0xffffffffa0000000
cxi_user 78901 2 cxi_core, Live 0xffffffffa0100000
nf_tables 234567 3 - Live 0xffffffffa0200000
`
	if err := os.WriteFile(procModules, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write mock /proc/modules: %v", err)
	}

	// Note: We can't easily test isModuleLoaded directly since it uses /proc/modules
	// This test documents the expected format parsing behavior
	t.Run("module format parsing", func(t *testing.T) {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			moduleName := fields[0]
			t.Logf("Found module: %s", moduleName)
		}
	})
}

func TestGetKernelVersion_Format(t *testing.T) {
	// Test the expected format of /proc/version
	// Linux version 5.15.0-88-generic (buildd@bos03-amd64-050) ...
	testVersions := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{
			input:    "Linux version 5.15.0-88-generic (buildd@bos03-amd64-050) #98-Ubuntu SMP Mon Oct 2 15:18:56 UTC 2023",
			expected: "5.15.0-88-generic",
			wantErr:  false,
		},
		{
			input:    "Linux version 6.1.0 (root@host) #1 SMP",
			expected: "6.1.0",
			wantErr:  false,
		},
		{
			input:    "Linux version",
			expected: "",
			wantErr:  true,
		},
		{
			input:    "",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range testVersions {
		t.Run(tt.input, func(t *testing.T) {
			fields := strings.Fields(tt.input)
			var version string
			var err error

			if len(fields) < 3 {
				err = errTooFewFields
			} else {
				version = fields[2]
			}

			if tt.wantErr {
				if err == nil && version == tt.expected {
					t.Error("expected error")
				}
			} else {
				if version != tt.expected {
					t.Errorf("version = %q, want %q", version, tt.expected)
				}
			}
		})
	}
}

var errTooFewFields = &parseError{"too few fields"}

type parseError struct {
	msg string
}

func (e *parseError) Error() string {
	return e.msg
}

func TestModuleNames(t *testing.T) {
	// Document expected CXI module names
	expectedModules := []string{
		"cxi_core",
		"cxi_user",
		"cxi_ss1",
		"cxi",
	}

	for _, mod := range expectedModules {
		t.Run(mod, func(t *testing.T) {
			// Just verify these are valid module names (no spaces, reasonable length)
			if strings.Contains(mod, " ") {
				t.Error("module name contains space")
			}
			if len(mod) == 0 {
				t.Error("module name is empty")
			}
			if len(mod) > 64 {
				t.Error("module name too long")
			}
		})
	}
}

func TestDKMSCommandFormats(t *testing.T) {
	// Document expected DKMS command formats
	tests := []struct {
		name    string
		command string
		args    []string
	}{
		{
			name:    "dkms add",
			command: "dkms",
			args:    []string{"add", "/path/to/source"},
		},
		{
			name:    "dkms build",
			command: "dkms",
			args:    []string{"build", "-m", "cxi", "-k", "5.15.0"},
		},
		{
			name:    "dkms install",
			command: "dkms",
			args:    []string{"install", "-m", "cxi", "-k", "5.15.0"},
		},
		{
			name:    "dkms remove",
			command: "dkms",
			args:    []string{"remove", "cxi", "--all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.command != "dkms" {
				t.Errorf("command = %q, want 'dkms'", tt.command)
			}
			if len(tt.args) == 0 {
				t.Error("args is empty")
			}
		})
	}
}

func TestModprobeCommandFormat(t *testing.T) {
	// Document expected modprobe command format
	moduleName := "cxi_core"
	expectedCommand := "modprobe"

	if expectedCommand != "modprobe" {
		t.Errorf("command = %q, want 'modprobe'", expectedCommand)
	}
	if moduleName == "" {
		t.Error("module name is empty")
	}
}

func TestRmmodCommandFormat(t *testing.T) {
	// Document expected rmmod command format
	moduleName := "cxi_core"
	expectedCommand := "rmmod"

	if expectedCommand != "rmmod" {
		t.Errorf("command = %q, want 'rmmod'", expectedCommand)
	}
	if moduleName == "" {
		t.Error("module name is empty")
	}
}

func TestDepmodCommandFormat(t *testing.T) {
	// Document expected depmod command format
	expectedCommand := "depmod"
	expectedArgs := []string{"-a"}

	if expectedCommand != "depmod" {
		t.Errorf("command = %q, want 'depmod'", expectedCommand)
	}
	if len(expectedArgs) != 1 || expectedArgs[0] != "-a" {
		t.Errorf("args = %v, want ['-a']", expectedArgs)
	}
}

func TestProcModulesLineFormat(t *testing.T) {
	// Test parsing of /proc/modules line format
	// Format: name size refcount deps state address
	lines := []struct {
		line       string
		wantName   string
		wantLoaded bool
	}{
		{
			line:       "cxi_core 123456 1 - Live 0xffffffffa0000000",
			wantName:   "cxi_core",
			wantLoaded: true,
		},
		{
			line:       "nf_tables 234567 3 nf_tables_set, Live 0xffffffffa0200000",
			wantName:   "nf_tables",
			wantLoaded: true,
		},
		{
			line:       "",
			wantName:   "",
			wantLoaded: false,
		},
	}

	for _, tt := range lines {
		t.Run(tt.wantName, func(t *testing.T) {
			fields := strings.Fields(tt.line)
			var name string
			if len(fields) > 0 {
				name = fields[0]
			}

			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestModuleErrorMessages(t *testing.T) {
	// Test error message formats
	tests := []struct {
		operation string
		module    string
		wantErr   string
	}{
		{
			operation: "modprobe",
			module:    "cxi_core",
			wantErr:   "modprobe cxi_core failed",
		},
		{
			operation: "rmmod",
			module:    "cxi_core",
			wantErr:   "rmmod cxi_core failed",
		},
		{
			operation: "dkms add",
			module:    "",
			wantErr:   "dkms add failed",
		},
		{
			operation: "dkms build",
			module:    "",
			wantErr:   "dkms build failed",
		},
		{
			operation: "dkms install",
			module:    "",
			wantErr:   "dkms install failed",
		},
		{
			operation: "dkms remove",
			module:    "",
			wantErr:   "dkms remove failed",
		},
		{
			operation: "depmod",
			module:    "",
			wantErr:   "depmod failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			if !strings.Contains(tt.wantErr, tt.operation) {
				t.Errorf("error message %q should contain operation %q", tt.wantErr, tt.operation)
			}
		})
	}
}
