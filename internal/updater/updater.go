// Package updater implements in-place self-update of the CLI binary from the
// public GitHub releases of nanostack-dev/echopoint-cli.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	releasesAPI       = "https://api.github.com/repos/nanostack-dev/echopoint-cli/releases/latest"
	runnerReleasesAPI = "https://api.github.com/repos/nanostack-dev/echopoint-runner/releases/latest"
	binaryName        = "echopoint"
	runnerBinaryName  = "echopoint-runner"
	requestTimeout    = 60 * time.Second
)

// Release is the subset of the GitHub release payload we use.
type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// LatestRelease fetches the newest published CLI release from GitHub.
func LatestRelease(ctx context.Context) (*Release, error) {
	return latestReleaseFrom(ctx, releasesAPI)
}

// LatestRunnerRelease fetches the newest published echopoint-runner release.
func LatestRunnerRelease(ctx context.Context) (*Release, error) {
	return latestReleaseFrom(ctx, runnerReleasesAPI)
}

// latestReleaseFrom fetches the newest published release from the given GitHub
// releases API URL (anonymous; the repositories are public).
func latestReleaseFrom(ctx context.Context, apiURL string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "echopoint-cli")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("latest release has no tag")
	}
	return &release, nil
}

// archiveExt is the asset extension for the current platform.
func archiveExt() string {
	if runtime.GOOS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}

// findAsset selects the release asset for the running OS/arch.
func (r *Release) findAsset() (*Asset, error) {
	suffix := fmt.Sprintf("_%s_%s%s", runtime.GOOS, runtime.GOARCH, archiveExt())
	for i := range r.Assets {
		if strings.HasSuffix(r.Assets[i].Name, suffix) {
			return &r.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("no release asset for %s/%s in %s", runtime.GOOS, runtime.GOARCH, r.TagName)
}

func (r *Release) checksumsAsset() *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == "checksums.txt" {
			return &r.Assets[i]
		}
	}
	return nil
}

// IsNewer reports whether tag (e.g. "v0.3.0") is a strictly higher semantic
// version than current (e.g. "v0.2.0" or "dev"). A non-semver current version
// (dev/source build) is always considered older so updates are allowed.
func IsNewer(current, tag string) bool {
	cur, okCur := parseSemver(current)
	next, okNext := parseSemver(tag)
	if !okNext {
		return false
	}
	if !okCur {
		return true
	}
	for i := range 3 {
		if next[i] != cur[i] {
			return next[i] > cur[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i] // drop prerelease/build metadata
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "echopoint-cli")

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// verifyChecksum checks the archive bytes against the sha256 listed for
// assetName in a goreleaser-style checksums.txt ("<hex>  <name>" per line).
func verifyChecksum(checksums []byte, assetName string, archive []byte) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum listed for %s", assetName)
	}
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", assetName, got, want)
	}
	return nil
}

// extractBinary pulls the named binary out of the downloaded archive.
func extractBinary(assetName string, archive []byte, wantBinary string) ([]byte, error) {
	wantNames := map[string]bool{wantBinary: true, wantBinary + ".exe": true}

	if strings.HasSuffix(assetName, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if wantNames[path.Base(f.Name)] {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("binary %q not found in archive", wantBinary)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && wantNames[path.Base(hdr.Name)] {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", wantBinary)
}

// Apply downloads the asset for this platform from the given release, verifies
// its checksum, extracts the binary, and replaces the running executable.
func Apply(ctx context.Context, release *Release) error {
	asset, err := release.findAsset()
	if err != nil {
		return err
	}

	archive, err := download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}

	if cs := release.checksumsAsset(); cs != nil {
		checksums, derr := download(ctx, cs.BrowserDownloadURL)
		if derr != nil {
			return fmt.Errorf("download checksums: %w", derr)
		}
		if verr := verifyChecksum(checksums, asset.Name, archive); verr != nil {
			return verr
		}
	}

	binary, err := extractBinary(asset.Name, archive, binaryName)
	if err != nil {
		return err
	}

	if err := selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("update failed and rollback failed (rollback: %w): %w", rerr, err)
		}
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// RunnerBinaryPath resolves where the echopoint-runner binary should be
// installed: an existing one already on PATH, otherwise alongside the running
// CLI executable (so a single install location holds both).
func RunnerBinaryPath() (string, error) {
	if p, err := exec.LookPath(runnerBinaryName); err == nil {
		return p, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate CLI executable: %w", err)
	}
	name := runnerBinaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(self), name), nil
}

// ApplyRunner downloads the echopoint-runner asset for this platform from
// release, verifies its checksum, extracts the runner binary, and installs it
// atomically at destPath. Unlike Apply it does not touch the running process —
// the runner is a separate binary the CLI shells out to.
func ApplyRunner(ctx context.Context, release *Release, destPath string) error {
	asset, err := release.findAsset()
	if err != nil {
		return err
	}

	archive, err := download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset.Name, err)
	}

	if cs := release.checksumsAsset(); cs != nil {
		checksums, derr := download(ctx, cs.BrowserDownloadURL)
		if derr != nil {
			return fmt.Errorf("download checksums: %w", derr)
		}
		if verr := verifyChecksum(checksums, asset.Name, archive); verr != nil {
			return verr
		}
	}

	binary, err := extractBinary(asset.Name, archive, runnerBinaryName)
	if err != nil {
		return err
	}

	return installBinary(destPath, binary)
}

// installBinary writes b to destPath atomically (temp file in the same
// directory, then rename) with executable permissions.
func installBinary(destPath string, b []byte) error {
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".echopoint-runner-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write runner binary: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod runner binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close runner binary: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("install runner at %s: %w", destPath, err)
	}
	return nil
}
