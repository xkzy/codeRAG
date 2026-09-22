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
	"time"
)

const (
	GitHubAPIBase = "https://api.github.com"
	DefaultRepo   = "xkzy/codeRAG"
)

type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	Assets     []Asset `json:"assets"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL    string  `json:"html_url"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

type UpdateInfo struct {
	CurrentVersion string
	LatestVersion  string
	Release        *Release
	Asset          *Asset
	DownloadURL    string
	ReleaseNotes   string
}

func CheckForUpdate(currentVersion, repo string) (*Release, error) {
	if repo == "" {
		repo = DefaultRepo
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", GitHubAPIBase, repo)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "codergag-selfupdate")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("repository not found or no releases")
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}
	
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	
	// Skip drafts and prereleases unless current is also prerelease
	if release.Draft || release.Prerelease {
		if !strings.Contains(currentVersion, "-") {
			return nil, nil
		}
	}
	
	if release.TagName == currentVersion {
		return nil, nil // No update available
	}
	
	return &release, nil
}

func FindMatchingAsset(release *Release) *Asset {
	platform := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	
	for i := range release.Assets {
		name := release.Assets[i].Name
		if strings.Contains(name, platform) && 
		   (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip")) {
			return &release.Assets[i]
		}
	}
	
	// Fallback: try without extension match
	for i := range release.Assets {
		if strings.Contains(release.Assets[i].Name, platform) {
			return &release.Assets[i]
		}
	}
	
	return nil
}

func GetUpdateInfo(currentVersion, repo string) (*UpdateInfo, error) {
	release, err := CheckForUpdate(currentVersion, repo)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}
	
	asset := FindMatchingAsset(release)
	if asset == nil {
		return nil, fmt.Errorf("no matching asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	
	return &UpdateInfo{
		CurrentVersion: currentVersion,
		LatestVersion:  release.TagName,
		Release:        release,
		Asset:          asset,
		DownloadURL:    asset.BrowserDownloadURL,
		ReleaseNotes:   release.Body,
	}, nil
}

func DownloadAsset(url, destPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "codergag-selfupdate")
	
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: %d", resp.StatusCode)
	}
	
	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	
	written, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	if written != resp.ContentLength && resp.ContentLength > 0 {
		os.Remove(tmpPath)
		return fmt.Errorf("download incomplete: got %d, expected %d", written, resp.ContentLength)
	}
	
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	return os.Rename(tmpPath, destPath)
}

func InstallUpdate(binaryPath string, info *UpdateInfo) error {
	if info == nil || info.Asset == nil {
		return fmt.Errorf("no update info provided")
	}
	
	// Download to temp location
	tmpDir := filepath.Dir(binaryPath)
	tmpBinary := filepath.Join(tmpDir, "codergag.new")
	
	if err := DownloadAsset(info.DownloadURL, tmpBinary); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	
	// Verify it's executable
	if err := verifyBinary(tmpBinary); err != nil {
		os.Remove(tmpBinary)
		return fmt.Errorf("binary verification failed: %w", err)
	}
	
	// Backup current binary
	backupPath := binaryPath + ".bak"
	if err := os.Rename(binaryPath, backupPath); err != nil {
		os.Remove(tmpBinary)
		return fmt.Errorf("backup failed: %w", err)
	}
	
	// Atomic replace
	if err := os.Rename(tmpBinary, binaryPath); err != nil {
		// Try to restore backup
		os.Rename(backupPath, binaryPath)
		return fmt.Errorf("install failed: %w", err)
	}
	
	// Clean up backup
	os.Remove(backupPath)
	
	return nil
}

func verifyBinary(path string) error {
	// Basic verification: check it's executable and runs --version
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("not executable")
	}
	return nil
}

func GetCurrentBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolve symlinks
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe, nil
	}
	return real, nil
}