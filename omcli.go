package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed embed/bin/om-darwin-amd64
var omDarwinAMD64 []byte

//go:embed embed/bin/om-darwin-arm64
var omDarwinARM64 []byte

//go:embed embed/bin/om-linux-amd64
var omLinuxAMD64 []byte

//go:embed embed/bin/om-windows-amd64.exe
var omWindowsAMD64 []byte

var (
	omPath     string
	omPathOnce sync.Once
	omPathErr  error
)

// GetOMPath returns the path to the OM CLI binary, extracting it if necessary
func GetOMPath() (string, error) {
	omPathOnce.Do(func() {
		// Determine which binary to use based on OS and architecture
		var omBinary []byte
		var fileName string

		switch runtime.GOOS {
		case "darwin":
			switch runtime.GOARCH {
			case "arm64":
				omBinary = omDarwinARM64
				fileName = "om"
			case "amd64":
				omBinary = omDarwinAMD64
				fileName = "om"
			default:
				omPathErr = fmt.Errorf("unsupported darwin architecture: %s", runtime.GOARCH)
				return
			}
		case "linux":
			if runtime.GOARCH == "amd64" {
				omBinary = omLinuxAMD64
				fileName = "om"
			} else {
				omPathErr = fmt.Errorf("unsupported linux architecture: %s", runtime.GOARCH)
				return
			}
		case "windows":
			if runtime.GOARCH == "amd64" {
				omBinary = omWindowsAMD64
				fileName = "om.exe"
			} else {
				omPathErr = fmt.Errorf("unsupported windows architecture: %s", runtime.GOARCH)
				return
			}
		default:
			omPathErr = fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
			return
		}

		// Create a temporary directory for the OM binary
		tempDir, err := os.MkdirTemp("", "tanzu-downloader-om-*")
		if err != nil {
			omPathErr = fmt.Errorf("failed to create temp directory: %w", err)
			return
		}

		// Write the binary to the temp directory
		omPath = filepath.Join(tempDir, fileName)
		if err := os.WriteFile(omPath, omBinary, 0755); err != nil {
			omPathErr = fmt.Errorf("failed to write OM binary: %w", err)
			return
		}

		fmt.Printf("OM CLI extracted to: %s\n", omPath)
	})

	return omPath, omPathErr
}
