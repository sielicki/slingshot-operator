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
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadFile_Success(t *testing.T) {
	content := []byte("test file content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "downloaded_file")
	err := downloadFile(server.URL+"/file", dest)
	if err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("file content = %q, want %q", data, content)
	}
}

func TestDownloadFile_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "downloaded_file")
	err := downloadFile(server.URL+"/file", dest)
	if err == nil {
		t.Error("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want to contain '404'", err)
	}
}

func TestDownloadFile_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "downloaded_file")
	err := downloadFile(server.URL+"/file", dest)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestDownloadFile_CreatesDirectory(t *testing.T) {
	content := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer server.Close()

	// Create a path with nested directories that don't exist
	dest := filepath.Join(t.TempDir(), "a", "b", "c", "file")
	err := downloadFile(server.URL+"/file", dest)
	if err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}

	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Error("file was not created")
	}
}

func TestDownloadFile_ConnectionError(t *testing.T) {
	// Use a URL that won't connect
	dest := filepath.Join(t.TempDir(), "file")
	err := downloadFile("http://localhost:1/file", dest)
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestExtractTar_NormalFiles(t *testing.T) {
	// Create a tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add a file
	fileContent := []byte("hello world")
	hdr := &tar.Header{
		Name: "test.txt",
		Mode: 0644,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(fileContent); err != nil {
		t.Fatal(err)
	}

	// Add a directory
	dirHdr := &tar.Header{
		Name:     "subdir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	if err := tw.WriteHeader(dirHdr); err != nil {
		t.Fatal(err)
	}

	// Add a file in the directory
	subfileContent := []byte("subfile content")
	subfileHdr := &tar.Header{
		Name: "subdir/subfile.txt",
		Mode: 0644,
		Size: int64(len(subfileContent)),
	}
	if err := tw.WriteHeader(subfileHdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(subfileContent); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	// Extract
	dest := t.TempDir()
	if err := extractTar(&buf, dest); err != nil {
		t.Fatalf("extractTar returned error: %v", err)
	}

	// Verify files
	data, err := os.ReadFile(filepath.Join(dest, "test.txt"))
	if err != nil {
		t.Fatalf("failed to read test.txt: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("test.txt content = %q, want %q", data, fileContent)
	}

	data, err = os.ReadFile(filepath.Join(dest, "subdir", "subfile.txt"))
	if err != nil {
		t.Fatalf("failed to read subdir/subfile.txt: %v", err)
	}
	if !bytes.Equal(data, subfileContent) {
		t.Errorf("subdir/subfile.txt content = %q, want %q", data, subfileContent)
	}
}

func TestExtractTar_PathTraversalPrevention(t *testing.T) {
	// Create a tar archive with path traversal attempt
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	hdr := &tar.Header{
		Name: "../../../etc/passwd",
		Mode: 0644,
		Size: 4,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	err := extractTar(&buf, dest)
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
	if !strings.Contains(err.Error(), "invalid tar path") {
		t.Errorf("error = %v, want to contain 'invalid tar path'", err)
	}
}

func TestExtractTar_Symlinks(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add a regular file first
	fileContent := []byte("target content")
	fileHdr := &tar.Header{
		Name: "target.txt",
		Mode: 0644,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(fileHdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(fileContent); err != nil {
		t.Fatal(err)
	}

	// Add a symlink
	linkHdr := &tar.Header{
		Name:     "link.txt",
		Linkname: "target.txt",
		Typeflag: tar.TypeSymlink,
	}
	if err := tw.WriteHeader(linkHdr); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := extractTar(&buf, dest); err != nil {
		t.Fatalf("extractTar returned error: %v", err)
	}

	// Verify symlink exists
	linkPath := filepath.Join(dest, "link.txt")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("failed to stat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink")
	}

	// Verify symlink target
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("failed to read link: %v", err)
	}
	if target != "target.txt" {
		t.Errorf("link target = %q, want %q", target, "target.txt")
	}
}

func TestExtractTar_EmptyArchive(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	err := extractTar(&buf, dest)
	if err != nil {
		t.Errorf("extractTar returned error for empty archive: %v", err)
	}
}

func TestDownloadAndExtract_TarGz(t *testing.T) {
	// Create a gzipped tar archive
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	fileContent := []byte("gzipped content")
	hdr := &tar.Header{
		Name: "file.txt",
		Mode: 0644,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(fileContent); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer server.Close()

	dest := t.TempDir()
	err := downloadAndExtract(server.URL+"/archive.tar.gz", dest)
	if err != nil {
		t.Fatalf("downloadAndExtract returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("failed to read file.txt: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("file.txt content = %q, want %q", data, fileContent)
	}
}

func TestDownloadAndExtract_Tgz(t *testing.T) {
	// Create a gzipped tar archive
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	fileContent := []byte("tgz content")
	hdr := &tar.Header{
		Name: "file.txt",
		Mode: 0644,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(fileContent); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer server.Close()

	dest := t.TempDir()
	err := downloadAndExtract(server.URL+"/archive.tgz", dest)
	if err != nil {
		t.Fatalf("downloadAndExtract returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "file.txt")); os.IsNotExist(err) {
		t.Error("file.txt was not extracted")
	}
}

func TestDownloadAndExtract_UnsupportedFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not an archive"))
	}))
	defer server.Close()

	dest := t.TempDir()
	err := downloadAndExtract(server.URL+"/archive.rar", dest)
	if err == nil {
		t.Error("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported archive format") {
		t.Errorf("error = %v, want to contain 'unsupported archive format'", err)
	}
}

func TestDownloadAndExtract_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dest := t.TempDir()
	err := downloadAndExtract(server.URL+"/archive.tar.gz", dest)
	if err == nil {
		t.Error("expected error for HTTP 404")
	}
}

func TestDownloadAndExtract_InvalidGzip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid gzip data"))
	}))
	defer server.Close()

	dest := t.TempDir()
	err := downloadAndExtract(server.URL+"/archive.tar.gz", dest)
	if err == nil {
		t.Error("expected error for invalid gzip")
	}
}

func TestDownloadAndExtract_CreatesDirectory(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: "test.txt",
		Mode: 0644,
		Size: 4,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte("test"))
	tw.Close()
	gw.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "new", "nested", "dir")
	err := downloadAndExtract(server.URL+"/archive.tar.gz", dest)
	if err != nil {
		t.Fatalf("downloadAndExtract returned error: %v", err)
	}

	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Error("destination directory was not created")
	}
}

// Helper to create a simple tar reader for testing
func createSimpleTar(t *testing.T, files map[string]string) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	return &buf
}

func TestExtractTar_MultipleFiles(t *testing.T) {
	files := map[string]string{
		"file1.txt": "content 1",
		"file2.txt": "content 2",
		"file3.txt": "content 3",
	}
	reader := createSimpleTar(t, files)

	dest := t.TempDir()
	if err := extractTar(reader, dest); err != nil {
		t.Fatalf("extractTar returned error: %v", err)
	}

	for name, expectedContent := range files {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Errorf("failed to read %s: %v", name, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("%s content = %q, want %q", name, data, expectedContent)
		}
	}
}

// Helper to create a zip file for testing
func createTestZip(t *testing.T, files map[string]string) string {
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer tmpFile.Close()

	zw := zip.NewWriter(tmpFile)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return tmpFile.Name()
}

func TestExtractZip_NormalFiles(t *testing.T) {
	files := map[string]string{
		"file1.txt":        "content 1",
		"subdir/file2.txt": "content 2",
	}
	zipPath := createTestZip(t, files)

	dest := t.TempDir()
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("extractZip returned error: %v", err)
	}

	for name, expectedContent := range files {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Errorf("failed to read %s: %v", name, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("%s content = %q, want %q", name, data, expectedContent)
		}
	}
}

func TestExtractZip_PathTraversalPrevention(t *testing.T) {
	// Create a zip with path traversal attempt
	tmpFile, err := os.CreateTemp(t.TempDir(), "evil-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer tmpFile.Close()

	zw := zip.NewWriter(tmpFile)
	w, err := zw.Create("../../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("evil"))
	zw.Close()

	dest := t.TempDir()
	err = extractZip(tmpFile.Name(), dest)
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
	if !strings.Contains(err.Error(), "invalid zip path") {
		t.Errorf("error = %v, want to contain 'invalid zip path'", err)
	}
}

func TestDownloadAndExtract_Zip(t *testing.T) {
	// Create a zip archive in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, err := zw.Create("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	fileContent := []byte("zip content")
	w.Write(fileContent)
	zw.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	defer server.Close()

	dest := t.TempDir()
	err = downloadAndExtract(server.URL+"/archive.zip", dest)
	if err != nil {
		t.Fatalf("downloadAndExtract returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "test.txt"))
	if err != nil {
		t.Fatalf("failed to read test.txt: %v", err)
	}
	if !bytes.Equal(data, fileContent) {
		t.Errorf("test.txt content = %q, want %q", data, fileContent)
	}
}

func TestExtractVersionFromURL(t *testing.T) {
	tests := []struct {
		url     string
		want    string
	}{
		// Tag formats
		{"https://github.com/user/repo/archive/refs/tags/v1.2.3.zip", "1.2.3"},
		{"https://github.com/user/repo/archive/refs/tags/1.2.3.zip", "1.2.3"},
		{"https://github.com/user/repo/archive/refs/tags/v0.1.0-alpha.zip", "0.1.0-alpha"},

		// Branch formats
		{"https://github.com/user/repo/archive/refs/heads/main.zip", "main"},
		{"https://github.com/user/repo/archive/refs/heads/develop.zip", "develop"},
		{"https://github.com/user/repo/archive/refs/heads/feature-branch.zip", "feature-branch"},

		// Short formats
		{"https://github.com/user/repo/archive/v1.0.0.zip", "1.0.0"},
		{"https://github.com/user/repo/archive/main.zip", "main"},

		// Unknown format
		{"https://example.com/file.zip", "0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractVersionFromURL(tt.url)
			if got != tt.want {
				t.Errorf("extractVersionFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestPrepareDKMSSource_FlattenGitHubStructure(t *testing.T) {
	// Create a directory structure like GitHub zip extracts to
	base := t.TempDir()
	nestedDir := filepath.Join(base, "repo-main")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create files in the nested directory
	if err := os.WriteFile(filepath.Join(nestedDir, "README.md"), []byte("# Readme"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(nestedDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "src", "main.c"), []byte("int main() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run prepareDKMSSource
	if err := prepareDKMSSource(base, "test-pkg", "1.0.0"); err != nil {
		t.Fatalf("prepareDKMSSource returned error: %v", err)
	}

	// Verify files were moved up
	if _, err := os.Stat(filepath.Join(base, "README.md")); os.IsNotExist(err) {
		t.Error("README.md should be at top level")
	}
	if _, err := os.Stat(filepath.Join(base, "src", "main.c")); os.IsNotExist(err) {
		t.Error("src/main.c should exist")
	}

	// Verify nested directory was removed
	if _, err := os.Stat(nestedDir); !os.IsNotExist(err) {
		t.Error("nested directory should be removed")
	}
}

func TestPrepareDKMSSource_GenerateDKMSConf(t *testing.T) {
	base := t.TempDir()

	// Create dkms.conf.in
	dkmsConfIn := `PACKAGE_NAME="@PACKAGE_NAME@"
PACKAGE_VERSION="@PACKAGE_VERSION@"
SHS_DKMS_AUX_DIR="@SHS_DKMS_AUX_DIR@"
MAKE="make"
`
	if err := os.WriteFile(filepath.Join(base, "dkms.conf.in"), []byte(dkmsConfIn), 0644); err != nil {
		t.Fatal(err)
	}

	// Run prepareDKMSSource
	if err := prepareDKMSSource(base, "cxi-driver", "2.5.0"); err != nil {
		t.Fatalf("prepareDKMSSource returned error: %v", err)
	}

	// Verify dkms.conf was created
	data, err := os.ReadFile(filepath.Join(base, "dkms.conf"))
	if err != nil {
		t.Fatalf("failed to read dkms.conf: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `PACKAGE_NAME="cxi-driver"`) {
		t.Error("PACKAGE_NAME not substituted correctly")
	}
	if !strings.Contains(content, `PACKAGE_VERSION="2.5.0"`) {
		t.Error("PACKAGE_VERSION not substituted correctly")
	}
	if !strings.Contains(content, `SHS_DKMS_AUX_DIR="/etc/shs-dkms"`) {
		t.Error("SHS_DKMS_AUX_DIR not substituted correctly")
	}
}

func TestGenerateDKMSConf(t *testing.T) {
	base := t.TempDir()
	inPath := filepath.Join(base, "dkms.conf.in")
	outPath := filepath.Join(base, "dkms.conf")

	// Create template
	template := `# DKMS Config
PACKAGE_NAME="@PACKAGE_NAME@"
PACKAGE_VERSION="@PACKAGE_VERSION@"
SHS_DKMS_AUX_DIR="@SHS_DKMS_AUX_DIR@"
BUILD_DEPENDS=("dep1" "dep2")
`
	if err := os.WriteFile(inPath, []byte(template), 0644); err != nil {
		t.Fatal(err)
	}

	// Generate
	if err := generateDKMSConf(inPath, outPath, "my-package", "3.0.0"); err != nil {
		t.Fatalf("generateDKMSConf returned error: %v", err)
	}

	// Verify
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	expected := `# DKMS Config
PACKAGE_NAME="my-package"
PACKAGE_VERSION="3.0.0"
SHS_DKMS_AUX_DIR="/etc/shs-dkms"
BUILD_DEPENDS=("dep1" "dep2")
`
	if string(data) != expected {
		t.Errorf("generated content:\n%s\nwant:\n%s", data, expected)
	}
}
