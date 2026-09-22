package main

import (
	"fmt"
	"os"
	"runtime"

	"codergag/internal/selfupdate"
)

// Version is set at build time via ldflags
var Version = "dev"

func runUpdate(args []string) int {
	force := false
	checkOnly := false
	
	for _, arg := range args {
		switch arg {
		case "--force", "-f":
			force = true
		case "--check", "-c":
			checkOnly = true
		case "--help", "-h":
			printUpdateUsage()
			return 0
		}
	}
	
	binaryPath, err := selfupdate.GetCurrentBinaryPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get binary path: %v\n", err)
		return 1
	}
	
	fmt.Printf("codergag %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Binary: %s\n", binaryPath)
	fmt.Println("Checking for updates...")
	
	info, err := selfupdate.GetUpdateInfo(Version, "xkzy/codeRAG")
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return 1
	}
	
	if info == nil {
		fmt.Println("✓ Already up to date")
		return 0
	}
	
	fmt.Printf("Update available: %s -> %s\n", Version, info.LatestVersion)
	fmt.Printf("Asset: %s (%.1f MB)\n", info.Asset.Name, float64(info.Asset.Size)/1024/1024)
	
	if info.ReleaseNotes != "" {
		fmt.Printf("\nRelease notes:\n%s\n", truncate(info.ReleaseNotes, 800))
	}
	
	if checkOnly {
		return 0
	}
	
	if !force {
		fmt.Print("\nInstall update? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Update cancelled")
			return 0
		}
	}
	
	fmt.Println("Downloading and installing update...")
	if err := selfupdate.InstallUpdate(binaryPath, info); err != nil {
		fmt.Fprintf(os.Stderr, "install failed: %v\n", err)
		return 1
	}
	
	fmt.Println("✓ Update installed successfully")
	fmt.Printf("Restart codergag to use version %s\n", info.LatestVersion)
	
	return 0
}

func runUpdateCheck(args []string) int {
	info, err := selfupdate.GetUpdateInfo(Version, "xkzy/codeRAG")
	if err != nil {
		fmt.Fprintf(os.Stderr, "update check failed: %v\n", err)
		return 1
	}
	
	if info == nil {
		fmt.Printf("codergag %s: up to date\n", Version)
		return 0
	}
	
	fmt.Printf("Update available: %s -> %s\n", Version, info.LatestVersion)
	return 0
}

func printUpdateUsage() {
	fmt.Print(`Usage: codergag update [flags]

Check for and install updates from GitHub releases.

Flags:
  -c, --check    Check for updates only, don't install
  -f, --force    Install without confirmation prompt
  -h, --help     Show this help

Examples:
  codergag update          # Interactive update
  codergag update --check  # Check only
  codergag update --force  # Non-interactive install
`)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}