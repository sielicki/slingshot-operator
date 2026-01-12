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
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func downloadFile(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func downloadAndExtract(url, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// For zip files, we need to download to a temp file first (zip requires seeking)
	if strings.HasSuffix(url, ".zip") {
		tmpFile, err := os.CreateTemp("", "download-*.zip")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer func() { _ = os.Remove(tmpPath) }()

		if _, err := io.Copy(tmpFile, resp.Body); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to download zip: %w", err)
		}
		_ = tmpFile.Close()

		return extractZip(tmpPath, dest)
	}

	var reader io.Reader = resp.Body

	if strings.HasSuffix(url, ".gz") || strings.HasSuffix(url, ".tgz") {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer func() { _ = gzReader.Close() }()
		reader = gzReader
	}

	if strings.HasSuffix(url, ".tar") || strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
		return extractTar(reader, dest)
	}

	return fmt.Errorf("unsupported archive format: %s", url)
}

func extractTar(reader io.Reader, dest string) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		target := filepath.Join(dest, header.Name)

		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			_ = file.Close()
		case tar.TypeSymlink:
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}

	return nil
}

func extractZip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)

		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)) {
			return fmt.Errorf("invalid zip path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return fmt.Errorf("failed to open zip entry: %w", err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			_ = rc.Close()
			_ = outFile.Close()
			return fmt.Errorf("failed to write file: %w", err)
		}

		_ = rc.Close()
		_ = outFile.Close()
	}

	return nil
}

// extractVersionFromURL extracts a version string from a GitHub archive URL.
// Supports formats like:
//   - https://github.com/user/repo/archive/refs/tags/v1.2.3.zip -> "1.2.3"
//   - https://github.com/user/repo/archive/refs/heads/main.zip -> "main"
//   - https://github.com/user/repo/archive/v1.2.3.zip -> "1.2.3"
func extractVersionFromURL(url string) string {
	// Try to match tag format: /refs/tags/v1.2.3.zip or /refs/tags/1.2.3.zip
	tagRegex := regexp.MustCompile(`/refs/tags/v?([^/]+)\.zip$`)
	if matches := tagRegex.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	// Try to match branch format: /refs/heads/main.zip
	branchRegex := regexp.MustCompile(`/refs/heads/([^/]+)\.zip$`)
	if matches := branchRegex.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	// Try to match short format: /archive/v1.2.3.zip
	shortRegex := regexp.MustCompile(`/archive/v?([^/]+)\.zip$`)
	if matches := shortRegex.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	return "0.0.0"
}

// prepareDKMSSource processes a GitHub repository download for DKMS.
// It handles:
// 1. GitHub's nested directory structure (repo-branch/ or repo-tag/)
// 2. Generating dkms.conf from dkms.conf.in with proper substitutions
func prepareDKMSSource(sourcePath, packageName, packageVersion string) error {
	// GitHub zips contain a single top-level directory like "repo-main/" or "repo-v1.0.0/"
	// Find and flatten it
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	// If there's exactly one directory entry, it's likely the GitHub nested structure
	if len(entries) == 1 && entries[0].IsDir() {
		nestedDir := filepath.Join(sourcePath, entries[0].Name())

		// Move contents up one level
		nestedEntries, err := os.ReadDir(nestedDir)
		if err != nil {
			return fmt.Errorf("failed to read nested directory: %w", err)
		}

		for _, entry := range nestedEntries {
			src := filepath.Join(nestedDir, entry.Name())
			dst := filepath.Join(sourcePath, entry.Name())
			if err := os.Rename(src, dst); err != nil {
				return fmt.Errorf("failed to move %s: %w", entry.Name(), err)
			}
		}

		// Remove the now-empty nested directory
		if err := os.Remove(nestedDir); err != nil {
			return fmt.Errorf("failed to remove nested directory: %w", err)
		}
	}

	// Process dkms.conf.in if it exists
	dkmsConfIn := filepath.Join(sourcePath, "dkms.conf.in")
	dkmsConf := filepath.Join(sourcePath, "dkms.conf")

	if _, err := os.Stat(dkmsConfIn); err == nil {
		if err := generateDKMSConf(dkmsConfIn, dkmsConf, packageName, packageVersion); err != nil {
			return fmt.Errorf("failed to generate dkms.conf: %w", err)
		}
	}

	return nil
}

// generateDKMSConf creates dkms.conf from dkms.conf.in by substituting template variables
func generateDKMSConf(inPath, outPath, packageName, packageVersion string) error {
	content, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("failed to read dkms.conf.in: %w", err)
	}

	result := string(content)
	result = strings.ReplaceAll(result, "@PACKAGE_NAME@", packageName)
	result = strings.ReplaceAll(result, "@PACKAGE_VERSION@", packageVersion)
	result = strings.ReplaceAll(result, "@SHS_DKMS_AUX_DIR@", "/etc/shs-dkms")

	if err := os.WriteFile(outPath, []byte(result), 0644); err != nil {
		return fmt.Errorf("failed to write dkms.conf: %w", err)
	}

	return nil
}

// buildDependency downloads, prepares, and builds a kernel module dependency (SBL or SL)
func (a *Agent) buildDependency(name, url string) error {
	destPath := filepath.Join("/tmp", name)

	// Clean up any previous build
	if err := os.RemoveAll(destPath); err != nil {
		a.log.V(1).Info("Failed to clean up previous build", "path", destPath, "error", err)
	}

	if err := downloadAndExtract(url, destPath); err != nil {
		return fmt.Errorf("failed to download %s: %w", name, err)
	}

	if err := prepareDKMSSource(destPath, name, ""); err != nil {
		return fmt.Errorf("failed to prepare %s source: %w", name, err)
	}

	env := map[string]string{}

	// SBL requires SBL_EXTERNAL_BUILD=1 and platform selection
	if name == "sbl" {
		env["SBL_EXTERNAL_BUILD"] = "1"

		// Set platform based on config
		switch a.config.DKMSPlatform {
		case DKMSPlatformRosetta:
			env["PLATFORM_ROSETTA_HW"] = "1"
		case DKMSPlatformCassini:
			fallthrough
		default:
			env["PLATFORM_CASSINI_HW"] = "1"
		}

		// Point to cassini-headers install directory
		headersPath := "/tmp/cassini-headers/install/include"
		if _, err := os.Stat(headersPath); err == nil {
			// SBL Makefile uses KCPPFLAGS to find headers
			env["KCPPFLAGS"] = fmt.Sprintf("-I%s", headersPath)
		}
	}

	if err := buildKernelModule(destPath, env); err != nil {
		return fmt.Errorf("failed to build %s: %w", name, err)
	}

	a.log.Info("Built dependency", "name", name, "path", destPath)
	return nil
}

// buildCXIDriver downloads and builds the main CXI driver, linking against dependency symvers
func (a *Agent) buildCXIDriver(url string) error {
	sourcePath := "/tmp/cxi-driver"

	// Clean up any previous build
	if err := os.RemoveAll(sourcePath); err != nil {
		a.log.V(1).Info("Failed to clean up previous build", "path", sourcePath, "error", err)
	}

	if err := downloadAndExtract(url, sourcePath); err != nil {
		return fmt.Errorf("failed to download CXI driver: %w", err)
	}

	pkgName := a.config.DKMSPkgName
	if pkgName == "" {
		pkgName = "cxi-driver"
	}

	// Extract version from tag (e.g., "release/shs-12.0.2" -> "12.0.2")
	pkgVersion := a.config.DKMSPkgVersion
	if pkgVersion == "" && a.config.DKMSTag != "" {
		// Extract version from tag like "release/shs-12.0.2"
		parts := strings.Split(a.config.DKMSTag, "-")
		if len(parts) > 0 {
			pkgVersion = parts[len(parts)-1]
		}
	}
	if pkgVersion == "" {
		pkgVersion = extractVersionFromURL(url)
	}

	a.log.Info("Preparing CXI driver", "package", pkgName, "version", pkgVersion)

	if err := prepareDKMSSource(sourcePath, pkgName, pkgVersion); err != nil {
		return fmt.Errorf("failed to prepare CXI driver source: %w", err)
	}

	// Collect symvers from dependencies
	var symversPaths []string
	sblSymvers := getSymversPath("/tmp/sbl")
	slSymvers := getSymversPath("/tmp/sl")

	if _, err := os.Stat(sblSymvers); err == nil {
		symversPaths = append(symversPaths, sblSymvers)
	}
	if _, err := os.Stat(slSymvers); err == nil {
		symversPaths = append(symversPaths, slSymvers)
	}

	// Set KBUILD_EXTRA_SYMBOLS for the DKMS build
	if len(symversPaths) > 0 {
		symversEnv := strings.Join(symversPaths, " ")
		if err := os.Setenv("KBUILD_EXTRA_SYMBOLS", symversEnv); err != nil {
			a.log.V(1).Info("Failed to set KBUILD_EXTRA_SYMBOLS", "error", err)
		}
		a.log.Info("Set KBUILD_EXTRA_SYMBOLS", "paths", symversPaths)
	}

	if err := runDKMSAdd(sourcePath); err != nil {
		return fmt.Errorf("dkms add failed: %w", err)
	}

	if err := runDKMSBuild(pkgName); err != nil {
		return fmt.Errorf("dkms build failed: %w", err)
	}

	if err := runDKMSInstall(pkgName); err != nil {
		return fmt.Errorf("dkms install failed: %w", err)
	}

	a.log.Info("CXI driver build complete", "package", pkgName, "version", pkgVersion)
	return nil
}

// downloadHeaders downloads and prepares cassini-headers for the SBL build
func (a *Agent) downloadHeaders(url string) error {
	destPath := "/tmp/cassini-headers"

	// Clean up any previous download
	if err := os.RemoveAll(destPath); err != nil {
		a.log.V(1).Info("Failed to clean up previous headers", "path", destPath, "error", err)
	}

	if err := downloadAndExtract(url, destPath); err != nil {
		return fmt.Errorf("failed to download cassini-headers: %w", err)
	}

	// Flatten GitHub archive structure if needed
	if err := prepareDKMSSource(destPath, "cassini-headers", ""); err != nil {
		return fmt.Errorf("failed to prepare cassini-headers: %w", err)
	}

	// Create the install/include directory structure that SBL expects
	installDir := filepath.Join(destPath, "install", "include")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	// Copy headers from include/ to install/include/
	srcInclude := filepath.Join(destPath, "include")
	if _, err := os.Stat(srcInclude); err == nil {
		entries, err := os.ReadDir(srcInclude)
		if err != nil {
			return fmt.Errorf("failed to read include directory: %w", err)
		}
		for _, entry := range entries {
			src := filepath.Join(srcInclude, entry.Name())
			dst := filepath.Join(installDir, entry.Name())
			if err := copyDir(src, dst); err != nil {
				return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
			}
		}
	}

	a.log.Info("Prepared cassini-headers", "path", destPath)
	return nil
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			srcPath := filepath.Join(src, entry.Name())
			dstPath := filepath.Join(dst, entry.Name())
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		}
		return nil
	}

	// Copy file
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}
