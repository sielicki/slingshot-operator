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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func loadModule(name string) error {
	cmd := exec.Command("modprobe", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("modprobe %s failed: %w", name, err)
	}
	return nil
}

//nolint:unused // reserved for future cleanup operations
func unloadModule(name string) error {
	cmd := exec.Command("rmmod", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rmmod %s failed: %w", name, err)
	}
	return nil
}

func isModuleLoaded(name string) bool {
	file, err := os.Open("/proc/modules")
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

func getKernelVersion() (string, error) {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", fmt.Errorf("failed to read /proc/version: %w", err)
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return "", fmt.Errorf("unexpected /proc/version format")
	}

	return fields[2], nil
}

func runDepmod() error {
	cmd := exec.Command("depmod", "-a")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("depmod failed: %w", err)
	}
	return nil
}

func runDKMSAdd(sourcePath string) error {
	cmd := exec.Command("dkms", "add", sourcePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dkms add failed: %w", err)
	}
	return nil
}

func runDKMSBuild(moduleName string) error {
	kernelVersion, err := getKernelVersion()
	if err != nil {
		return err
	}

	cmd := exec.Command("dkms", "build", "-m", moduleName, "-k", kernelVersion)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dkms build failed: %w", err)
	}
	return nil
}

func runDKMSInstall(moduleName string) error {
	kernelVersion, err := getKernelVersion()
	if err != nil {
		return err
	}

	cmd := exec.Command("dkms", "install", "-m", moduleName, "-k", kernelVersion)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dkms install failed: %w", err)
	}
	return nil
}

//nolint:unused // reserved for future cleanup operations
func runDKMSRemove(moduleName string) error {
	cmd := exec.Command("dkms", "remove", moduleName, "--all")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dkms remove failed: %w", err)
	}
	return nil
}
