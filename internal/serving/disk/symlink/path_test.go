// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package symlink_test

import (
	archZip "archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/gitlab-org/gitlab-pages/internal/confighelper"
	"gitlab.com/gitlab-org/gitlab-pages/internal/serving/disk/symlink"
	"gitlab.com/gitlab-org/gitlab-pages/internal/testhelpers"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs"
	"gitlab.com/gitlab-org/gitlab-pages/internal/vfs/zip"
)

type EvalSymlinksTest struct {
	// If dest is empty, the path is created; otherwise the dest is symlinked to the path.
	path, dest string
}

var EvalSymlinksTestDirs = []EvalSymlinksTest{
	{"test", ""},
	{"test/dir", ""},
	{"test/dir/link3", "../../"},
	{"test/link1", "../test"},
	{"test/link2", "dir"},
	{"test/linkabs", "/"},
	{"test/link4", "../test2"},
	{"test2", "test/dir"},
	// Issue 23444.
	{"src", ""},
	{"src/pool", ""},
	{"src/pool/test", ""},
	{"src/versions", ""},
	{"src/versions/current", "../../version"},
	{"src/versions/v1", ""},
	{"src/versions/v1/modules", ""},
	{"src/versions/v1/modules/test", "../../../pool/test"},
	{"version", "src/versions/v1"},
}

var EvalSymlinksTests = []EvalSymlinksTest{
	{"test", "test"},
	{"test/dir", "test/dir"},
	{"test/dir/../..", "."},
	{"test/link1", "test"},
	{"test/link2", "test/dir"},
	{"test/link1/dir", "test/dir"},
	{"test/link2/..", "test"},
	{"test/dir/link3", "."},
	{"test/link2/link3/test", "test"},
	{"test/linkabs", "/"},
	{"test/link4/..", "test"},
	{"src/versions/current/modules/test", "src/pool/test"},
}

// generateZipContent generates the content of a zip file with symlinks
func generateZipContent(t *testing.T, symLinks []EvalSymlinksTest, files []string) ([]byte, error) {
	var err error
	buff := new(bytes.Buffer)
	zw := archZip.NewWriter(buff)

	for _, symLink := range symLinks {
		if symLink.dest == "" {
			_, err = zw.Create(symLink.path + "/")
			if err != nil {
				return nil, err
			}
			continue
		}

		// Write symlink
		h := &archZip.FileHeader{
			Name:     symLink.path,
			Method:   archZip.Deflate,
			Modified: time.Now(),
		}
		h.SetMode(os.ModeSymlink)

		header, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}

		_, err = header.Write([]byte(symLink.dest))
		if err != nil {
			return nil, err
		}
	}

	for _, file := range files {
		w, err := zw.Create(file)
		if err != nil {
			return nil, err
		}

		_, err = io.WriteString(w, "test")
		if err != nil {
			return nil, err
		}
	}
	err = zw.Close()
	if err != nil {
		return nil, err
	}
	zipBytes := buff.Bytes()
	debugZip(t, zipBytes)
	return zipBytes, nil
}

// createVFSRoot creates a VFS root from the served zip file
func createVFSRoot(url string, vfs vfs.VFS) (vfs.Root, error) {
	pathSha := testhelpers.Sha(url)
	return vfs.Root(context.Background(), url, pathSha)
}

// simpleJoin builds a file name from the directory and path.
// It does not use Join because we don't want ".." to be evaluated.
func simpleJoin(path ...string) string {
	return strings.Join(path, string(filepath.Separator))
}

func testEvalSymlinks(t *testing.T, wd, path, want string) {
	defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
	defer defaultZipVFS.Stop()

	root, err := createVFSRoot(wd, defaultZipVFS)
	require.NoError(t, err)

	have, err := symlink.EvalSymlinks(context.Background(), root, path)
	if err != nil {
		t.Errorf("evalSymlinks(%q) error: %v", path, err)
		return
	}
	if filepath.Clean(have) != filepath.Clean(want) {
		t.Errorf("evalSymlinks(%q) returns %q, want %q", path, have, want)
	}
}

func TestEvalSymlinks(t *testing.T) {
	content, err := generateZipContent(t, EvalSymlinksTestDirs, nil)
	require.NoError(t, err)
	server := testhelpers.ServeZipFile(content, "/")

	// Evaluate the symlink farm.
	for _, test := range EvalSymlinksTests {
		testEvalSymlinks(t, server.URL, test.path, test.dest)

		// test EvalSymlinks(".")
		testEvalSymlinks(t, simpleJoin(server.URL, test.path), ".", ".")

		// test EvalSymlinks("C:.") on Windows
		if runtime.GOOS == "windows" {
			volDot := filepath.VolumeName(server.URL) + "."
			testEvalSymlinks(t, simpleJoin(server.URL, test.path), volDot, volDot)
		}

		// test EvalSymlinks(".."+path)
		testEvalSymlinks(t,
			server.URL,
			simpleJoin("test", "..", test.path),
			test.dest)
	}
}

func TestEvalSymlinksIsNotExist(t *testing.T) {
	content, err := generateZipContent(t, EvalSymlinksTestDirs, nil)
	require.NoError(t, err)
	server := testhelpers.ServeZipFile(content, "/")

	defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
	defer defaultZipVFS.Stop()

	root, err := createVFSRoot(server.URL, defaultZipVFS)
	require.NoError(t, err)

	_, err = symlink.EvalSymlinks(context.Background(), root, "notexist")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected the file is not found, got %v\n", err)
	}

	err = os.Symlink("notexist", "link")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove("link")

	_, err = symlink.EvalSymlinks(context.Background(), root, "link")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected the file is not found, got %v\n", err)
	}
}

func TestIssue13582(t *testing.T) {
	evalSymlinksTestDirs := []EvalSymlinksTest{
		{"dir", ""},
		{"link_to_dir", "dir"},
		{"dir/link1", "../link_to_dir/file"},
		{"dir/link2", "../link_to_dir/link1"},
	}

	content, err := generateZipContent(t, evalSymlinksTestDirs, []string{"dir/file"})
	require.NoError(t, err)
	server := testhelpers.ServeZipFile(content, "/")

	defaultZipVFS := zip.New(&confighelper.DefaultZipCfgForTest)
	defer defaultZipVFS.Stop()

	root, err := createVFSRoot(server.URL, defaultZipVFS)
	require.NoError(t, err)

	tests := []struct {
		path, want string
	}{
		{"dir", "dir"},
		{"link_to_dir", "dir"},
		{"link_to_dir/file", "dir/file"},
		{"link_to_dir/link1", "dir/file"},
		{"link_to_dir/link2", "dir/file"},
	}
	for i, test := range tests {
		have, err := symlink.EvalSymlinks(context.Background(), root, test.path)
		if err != nil {
			t.Fatal(err)
		}
		if have != test.want {
			t.Errorf("test#%d: EvalSymlinks(%q) returns %q, want %q", i, test.path, have, test.want)
		}
	}
}

func debugZip(t *testing.T, zipBytes []byte) {
	readZip := bytes.NewReader(zipBytes)
	zipReader, err := archZip.NewReader(readZip, int64(len(zipBytes)))
	if err != nil {
		t.Errorf("Error inspecting ZIP: %v", err)
		return
	}

	t.Log("=== ZIP Content Summary ===")
	t.Logf("Total files in ZIP: %d", len(zipReader.File))
	for i, f := range zipReader.File {
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 {
			// Read the symlink target
			rc, err := f.Open()
			if err != nil {
				t.Errorf("  %d. SYMLINK: %s -> [error reading target: %v]", i, f.Name, err)
				continue
			}
			target, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Errorf("  %d. SYMLINK: %s -> [error reading target: %v]", i, f.Name, err)
				continue
			}
			t.Logf("  %d. SYMLINK: %s -> %s", i, f.Name, string(target))
		} else if mode.IsDir() {
			t.Logf("  %d. DIR: %s", i, f.Name)
		} else {
			t.Logf("  %d. FILE: %s (size: %d bytes)", i, f.Name, f.UncompressedSize64)
		}
	}
	t.Log("=== End ZIP Content Summary ===")

	// Uncomment below to write the ZIP file to /tmp for later inspection
	// tmpDir := "/tmp"
	// if err := os.MkdirAll(tmpDir, 0755); err != nil {
	// 	t.Logf("WARNING: Failed to create tmp directory: %v", err)
	// } else {
	// 	timestamp := time.Now().Format("20060102-150405")
	// 	zipFileName := filepath.Join(tmpDir, fmt.Sprintf("debug-symlinks-%s.zip", timestamp))
	// 	if err := os.WriteFile(zipFileName, zipBytes, 0644); err != nil {
	// 		t.Logf("WARNING: Failed to write ZIP file to %s: %v", zipFileName, err)
	// 	} else {
	// 		t.Logf("ZIP file written to %s for inspection", zipFileName)
	// 	}
	// }
}
