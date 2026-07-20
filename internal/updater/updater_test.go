package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, tag string
		want         bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"v0.2.0", "v0.2.1", true},
		{"v0.2.0", "v1.0.0", true},
		{"v0.3.0", "v0.2.0", false},
		{"v0.2.0", "v0.2.0", false},
		{"dev", "v0.2.0", true},      // source build is always older
		{"v0.2.0", "garbage", false}, // unparsable tag never triggers update
		{"v0.2.0-rc1", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.tag); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.tag, got, c.want)
		}
	}
}

func TestFindAssetMatchesPlatform(t *testing.T) {
	r := &Release{
		TagName: "v0.3.0",
		Assets: []Asset{
			{Name: "checksums.txt"},
			{Name: "echopoint_0.3.0_linux_amd64.tar.gz"},
			{Name: "echopoint_0.3.0_darwin_arm64.tar.gz"},
			{Name: "echopoint_0.3.0_windows_amd64.zip"},
		},
	}
	asset, err := r.findAsset()
	if err != nil {
		t.Fatalf("findAsset: %v", err)
	}
	// The selected asset must end with this platform's os_arch + ext.
	suffix := "_" + runtime.GOOS + "_" + runtime.GOARCH + archiveExt()
	if !strings.HasSuffix(asset.Name, suffix) {
		t.Errorf("selected asset %q does not match suffix %q", asset.Name, suffix)
	}
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("fake archive contents")
	sum := sha256.Sum256(archive)
	name := "echopoint_0.3.0_linux_amd64.tar.gz"
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")

	if err := verifyChecksum(checksums, name, archive); err != nil {
		t.Errorf("valid checksum rejected: %v", err)
	}
	if err := verifyChecksum(checksums, name, []byte("tampered")); err == nil {
		t.Error("tampered archive accepted")
	}
	if err := verifyChecksum(checksums, "missing.tar.gz", archive); err == nil {
		t.Error("missing checksum entry accepted")
	}
}

func TestExtractBinaryTarGz(t *testing.T) {
	want := []byte("#!/binary\x00contents")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string][]byte{
		"README.md": []byte("readme"),
		"echopoint": want,
	}
	for name, data := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(data)
	}
	_ = tw.Close()
	_ = gz.Close()

	got, err := extractBinary("echopoint_0.3.0_linux_amd64.tar.gz", buf.Bytes(), binaryName)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted binary mismatch")
	}
}

func TestInstallBinary(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "echopoint-runner")
	content := []byte("#!/bin/sh\necho runner\n")

	if err := installBinary(dest, content); err != nil {
		t.Fatalf("installBinary: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("installed content mismatch: got %q want %q", got, content)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("installed binary is not executable: mode %v", info.Mode())
	}

	// Overwriting an existing binary must succeed (atomic replace).
	if err := installBinary(dest, []byte("new")); err != nil {
		t.Fatalf("installBinary overwrite: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "new" {
		t.Fatalf("overwrite failed: got %q", got)
	}
}
