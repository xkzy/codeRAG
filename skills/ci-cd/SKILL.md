---
name: ci-cd
description: Use this skill when the user wants to set up, configure, or troubleshoot CI/CD pipelines. This includes GitHub Actions, GitLab CI, CircleCI, or other CI/CD systems. Also use for self-update mechanisms, release automation, build pipelines, and deployment workflows. Trigger on: "CI/CD", "pipeline", "GitHub Actions", "workflow", "self-update", "auto-update", "release automation", "build pipeline", "deployment".
---

# CI/CD Pipeline Skill

## Overview

This skill provides guidance for setting up and managing CI/CD pipelines with a focus on:
- GitHub Actions workflows
- Self-update mechanisms from GitHub releases
- Auto-update checking and installation
- Release automation
- Build and test pipelines

## Common Workflows

### 1. GitHub Actions Release Pipeline

Standard pattern for Go projects with multi-platform builds:

```yaml
# .github/workflows/release.yml
name: release

on:
  push:
    tags: ["v*"]
  workflow_dispatch:

permissions:
  contents: write

jobs:
  build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - {os: ubuntu-latest,    goos: linux,  goarch: amd64, ext: tar.gz}
          - {os: ubuntu-24.04-arm, goos: linux,  goarch: arm64, ext: tar.gz}
          - {os: macos-15-intel,   goos: darwin, goarch: amd64, ext: tar.gz}
          - {os: macos-latest,     goos: darwin, goarch: arm64, ext: tar.gz}
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install musl (Linux static builds)
        if: matrix.goos == 'linux'
        run: sudo apt-get update && sudo apt-get install -y musl-tools
      - name: Test
        if: matrix.goos == 'linux'
        run: go test ./...
      - name: Build
        shell: bash
        env:
          CGO_ENABLED: "1"
        run: |
          if [ "${{ matrix.goos }}" = linux ]; then
            export CC=musl-gcc
            go build -trimpath -ldflags '-s -w -linkmode external -extldflags "-static"' -o app ./cmd/app
            if ldd app 2>&1 | grep -qv "not a dynamic"; then echo "binary is not static"; ldd app; exit 1; fi
          else
            go build -trimpath -ldflags '-s -w' -o app ./cmd/app
          fi
          name="app_${{ matrix.goos }}_${{ matrix.goarch }}"
          tar czf "$name.tar.gz" app README.md LICENSE
          echo "ASSET=$name.tar.gz" >> "$GITHUB_ENV"
      - uses: actions/upload-artifact@v4
        with:
          name: ${{ env.ASSET }}
          path: ${{ env.ASSET }}

  release:
    needs: build
    if: startsWith(github.ref, 'refs/tags/v')
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: dist
          merge-multiple: true
      - name: Checksums
        run: cd dist && sha256sum * > SHA256SUMS
      - name: Publish
        env:
          GH_TOKEN: ${{ github.token }}
        run: gh release create "${{ github.ref_name }}" dist/* --repo "${{ github.repository }}" --title "${{ github.ref_name }}" --generate-notes
```

### 2. Self-Update from GitHub Release

Add self-update capability to your CLI application:

```go
// internal/selfupdate/selfupdate.go
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Assets     []Asset `json:"assets"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func CheckForUpdate(currentVersion, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	
	if release.TagName == currentVersion {
		return nil, nil // No update available
	}
	
	return &release, nil
}

func DownloadAndInstall(release *Release, currentBinaryPath string) error {
	// Find matching asset for current platform
	asset := findMatchingAsset(release)
	if asset == nil {
		return fmt.Errorf("no matching asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	
	// Download
	resp, err := http.Get(asset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// Write to temp file
	tmpPath := currentBinaryPath + ".new"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	// Atomic replace
	return os.Rename(tmpPath, currentBinaryPath)
}

func findMatchingAsset(release *Release) *Asset {
	platform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	for i := range release.Assets {
		if strings.Contains(release.Assets[i].Name, platform) {
			return &release.Assets[i]
		}
	}
	return nil
}
```

### 3. Auto-Update Check on Startup

```go
// internal/autoupdate/autoupdate.go
package autoupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
	
	"myapp/internal/selfupdate"
)

const checkInterval = 24 * time.Hour

func StartAutoUpdater(currentVersion, repo, binaryPath string) {
	go func() {
		// Initial check after short delay
		time.Sleep(5 * time.Second)
		checkAndNotify(currentVersion, repo, binaryPath)
		
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()
		
		for range ticker.C {
			checkAndNotify(currentVersion, repo, binaryPath)
		}
	}()
}

func checkAndNotify(currentVersion, repo, binaryPath string) {
	release, err := selfupdate.CheckForUpdate(currentVersion, repo)
	if err != nil {
		return // Silently ignore errors
	}
	if release == nil {
		return // No update
	}
	
	// Notify user (could write to config file, log, etc.)
	fmt.Fprintf(os.Stderr, "\nUpdate available: %s -> %s\n", currentVersion, release.TagName)
	fmt.Fprintf(os.Stderr, "Run '%s update' to install\n", filepath.Base(binaryPath))
}
```

## CI/CD Best Practices

1. **Static linking for Linux** - Use musl for portable binaries
2. **Test on Linux only** - macOS runners have path differences
3. **Generate checksums** - SHA256SUMS for verification
4. **Semantic versioning** - Tag releases as `v1.2.3`
5. **Auto-update opt-in** - Don't auto-install without user consent
6. **Fallback handling** - Gracefully handle missing network/API limits

## Verification Commands

```bash
# Test workflow syntax
gh workflow run release.yml

# Check release artifacts
gh release list --repo owner/repo

# Manual build test
go test ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CC=musl-gcc go build -ldflags '-s -w -linkmode external -extldflags "-static"' -o app ./cmd/app
```