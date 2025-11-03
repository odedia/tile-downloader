package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// BroadcomService handles interactions with the Broadcom Support Portal API
type BroadcomService struct {
	ctx               context.Context
	apiToken          string
	baseURL           string
	activeDownloads   map[int]*exec.Cmd // Track active download processes by fileID
	downloadsMutex    sync.Mutex        // Mutex to protect activeDownloads map
}

// Product represents a Tanzu product
type Product struct {
	ID          int    `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Release represents a product release/version
type Release struct {
	ID          int    `json:"id"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release_date"`
	Description string `json:"description"`
}

// ProductFile represents a downloadable file
type ProductFile struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	AWSObjectKey string `json:"aws_object_key"`
	FileType     string `json:"file_type"`
	FileVersion  string `json:"file_version"`
	MD5          string `json:"md5"`
	SHA256       string `json:"sha256"`
}

// EULA represents an End User License Agreement
type EULA struct {
	ID      int    `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Dependency represents a product dependency
type Dependency struct {
	Release Release `json:"release"`
}

// DependencySpecifier represents dependency version constraints
type DependencySpecifier struct {
	ID        int    `json:"id"`
	Specifier string `json:"specifier"`
	Product   struct {
		ID   int    `json:"id"`
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"product"`
}

// NewBroadcomService creates a new Broadcom API service
func NewBroadcomService() *BroadcomService {
	return &BroadcomService{
		baseURL:         "https://network.tanzu.vmware.com",
		activeDownloads: make(map[int]*exec.Cmd),
	}
}

// startup initializes the service with context and loads saved token
func (b *BroadcomService) startup(ctx context.Context) {
	b.ctx = ctx
	// Load saved token on startup
	b.loadToken()
}

// getConfigPath returns the path to the config file
func (b *BroadcomService) getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configDir := filepath.Join(homeDir, ".tanzu-downloader")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(configDir, "config.json"), nil
}

// Config represents the application configuration
type Config struct {
	APIToken         string `json:"api_token"`
	DownloadLocation string `json:"download_location"`
}

// loadConfig loads the configuration from disk
func (b *BroadcomService) loadConfig() (*Config, error) {
	configPath, err := b.getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default config
			return b.getDefaultConfig(), nil
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Set defaults if not present
	if config.DownloadLocation == "" {
		config.DownloadLocation = b.getDefaultDownloadLocation()
	}

	return &config, nil
}

// saveConfig saves the configuration to disk
func (b *BroadcomService) saveConfig(config *Config) error {
	configPath, err := b.getConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

// getDefaultDownloadLocation returns the default download location
func (b *BroadcomService) getDefaultDownloadLocation() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "./Downloads/Tanzu"
	}
	return filepath.Join(homeDir, "Downloads", "Tanzu")
}

// getDefaultConfig returns default configuration
func (b *BroadcomService) getDefaultConfig() *Config {
	return &Config{
		APIToken:         "",
		DownloadLocation: b.getDefaultDownloadLocation(),
	}
}

// loadToken loads the API token from disk
func (b *BroadcomService) loadToken() error {
	config, err := b.loadConfig()
	if err != nil {
		return err
	}

	b.apiToken = config.APIToken
	return nil
}

// saveToken saves the API token to disk
func (b *BroadcomService) saveToken() error {
	config, err := b.loadConfig()
	if err != nil {
		config = b.getDefaultConfig()
	}

	config.APIToken = b.apiToken
	return b.saveConfig(config)
}

// SetAPIToken sets the Broadcom API token and saves it to disk
func (b *BroadcomService) SetAPIToken(token string) error {
	b.apiToken = token
	return b.saveToken()
}

// GetAPIToken returns the current API token
func (b *BroadcomService) GetAPIToken() string {
	return b.apiToken
}

// GetDownloadLocation returns the configured download location
func (b *BroadcomService) GetDownloadLocation() (string, error) {
	config, err := b.loadConfig()
	if err != nil {
		return "", err
	}
	return config.DownloadLocation, nil
}

// SetDownloadLocation sets the download location
func (b *BroadcomService) SetDownloadLocation(location string) error {
	config, err := b.loadConfig()
	if err != nil {
		config = b.getDefaultConfig()
	}

	config.DownloadLocation = location
	return b.saveConfig(config)
}

// CancelDownload cancels an active download by killing the process
func (b *BroadcomService) CancelDownload(fileID int) error {
	b.downloadsMutex.Lock()
	cmd, exists := b.activeDownloads[fileID]
	if !exists {
		b.downloadsMutex.Unlock()
		return fmt.Errorf("no active download found for file ID %d", fileID)
	}
	// Don't delete yet - let the Wait() handle cleanup
	b.downloadsMutex.Unlock()

	// Kill the process
	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill download process: %w", err)
		}
	}

	// Emit cancellation event immediately
	runtime.EventsEmit(b.ctx, "download-cancelled", map[string]interface{}{
		"fileID": fileID,
	})

	return nil
}

// ListProducts retrieves all available products from Broadcom
func (b *BroadcomService) ListProducts() ([]Product, error) {
	if b.apiToken == "" {
		return nil, fmt.Errorf("API token not set")
	}

	req, err := http.NewRequest("GET", b.baseURL+"/api/v2/products", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Products []Product `json:"products"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Products, nil
}

// GetProductReleases retrieves all releases for a specific product
func (b *BroadcomService) GetProductReleases(productSlug string) ([]Release, error) {
	if b.apiToken == "" {
		return nil, fmt.Errorf("API token not set")
	}

	url := fmt.Sprintf("%s/api/v2/products/%s/releases", b.baseURL, productSlug)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Releases []Release `json:"releases"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Releases, nil
}

// GetReleaseEULA retrieves the EULA for a specific release
func (b *BroadcomService) GetReleaseEULA(productSlug string, releaseID int) (*EULA, error) {
	if b.apiToken == "" {
		return nil, fmt.Errorf("API token not set")
	}

	url := fmt.Sprintf("%s/api/v2/products/%s/releases/%d", b.baseURL, productSlug, releaseID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		EULA *EULA `json:"eula"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.EULA, nil
}

// GetReleaseFiles retrieves all files for a specific release
func (b *BroadcomService) GetReleaseFiles(productSlug string, releaseID int) ([]ProductFile, error) {
	if b.apiToken == "" {
		return nil, fmt.Errorf("API token not set")
	}

	url := fmt.Sprintf("%s/api/v2/products/%s/releases/%d/product_files", b.baseURL, productSlug, releaseID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ProductFiles []ProductFile `json:"product_files"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.ProductFiles, nil
}

// DownloadStemcellWithOM downloads a stemcell using the OM CLI
func (b *BroadcomService) DownloadStemcellWithOM(productSlug string, releaseVersion string, fileName string, awsObjectKey string, savePath string, fileID int) error {
	if b.apiToken == "" {
		return fmt.Errorf("API token not set")
	}

	// Get the bundled OM CLI path
	omPath, err := GetOMPath()
	if err != nil {
		return fmt.Errorf("failed to get OM CLI: %w", err)
	}

	// savePath is now the output directory (not a full file path)
	outputDir := savePath
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Initial progress will come from OM CLI output

	// For stemcells, we need to extract the iaas from the filename
	// Example: "bosh-stemcell-1.915-vsphere-esxi-ubuntu-jammy-go_agent.tgz"
	stemcellIaas := "vsphere" // Default to vsphere
	lowerName := strings.ToLower(fileName)
	if strings.Contains(lowerName, "vsphere") {
		stemcellIaas = "vsphere"
	} else if strings.Contains(lowerName, "aws") {
		stemcellIaas = "aws"
	} else if strings.Contains(lowerName, "azure") {
		stemcellIaas = "azure"
	} else if strings.Contains(lowerName, "google") {
		stemcellIaas = "google"
	}

	// Run om download-product
	// For stemcells downloaded as products, we use -f with a glob pattern that matches the IaaS
	// The fileName contains the display name like "Ubuntu Jammy Stemcell for vSphere 1.915"
	// We create a glob pattern that will match the actual file
	fileGlob := fmt.Sprintf("*%s*", stemcellIaas)

	cmd := exec.Command(omPath,
		"download-product",
		"-t", b.apiToken,
		"-p", productSlug,
		"--product-version", releaseVersion,
		"-f", fileGlob,
		"-o", outputDir,
	)

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start om command: %w", err)
	}

	// Store in active downloads
	b.downloadsMutex.Lock()
	b.activeDownloads[fileID] = cmd
	b.downloadsMutex.Unlock()

	// Read stdout (not used for progress but consume it)
	go func() {
		buffer := make([]byte, 1024)
		for {
			_, err := stdout.Read(buffer)
			if err != nil {
				break
			}
		}
	}()

	// Read stderr and parse progress (handle carriage returns)
	var stderrOutput string
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				stderrOutput += chunk

				// Split by both newline and carriage return to handle progress updates
				parts := strings.FieldsFunc(chunk, func(r rune) bool {
					return r == '\n' || r == '\r'
				})

				for _, part := range parts {
					if part == "" || !strings.Contains(part, "%") {
						continue
					}

					// Pattern: " 211.14 MiB / 18.47 GiB [>------]   1.12% 02m56s"
					// Extract: total size and percentage
					reWithSize := regexp.MustCompile(`[\d.]+\s+([KMG]i?B)\s*/\s*([\d.]+)\s+([KMG]i?B)\s+\[.*?\]\s+([\d.]+)%`)
					matches := reWithSize.FindStringSubmatch(part)

					if len(matches) >= 5 {
						totalSizeStr := matches[2]
						unit := matches[3]
						percentage, _ := strconv.ParseFloat(matches[4], 64)
						totalSize, _ := strconv.ParseFloat(totalSizeStr, 64)

						var totalBytes int64
						switch unit {
						case "GiB", "GB":
							totalBytes = int64(totalSize * 1024 * 1024 * 1024)
						case "MiB", "MB":
							totalBytes = int64(totalSize * 1024 * 1024)
						case "KiB", "KB":
							totalBytes = int64(totalSize * 1024)
						}

						runtime.EventsEmit(b.ctx, "download-progress", map[string]interface{}{
							"fileID":    fileID,
							"progress":  percentage,
							"totalSize": totalBytes,
							"status":    "Downloading...",
						})
					} else {
						// Fallback: extract percentage only
						re := regexp.MustCompile(`([\d.]+)%`)
						matches := re.FindStringSubmatch(part)
						if len(matches) > 1 {
							if percentage, err := strconv.ParseFloat(matches[1], 64); err == nil {
								runtime.EventsEmit(b.ctx, "download-progress", map[string]interface{}{
									"fileID":   fileID,
									"progress": percentage,
									"status":   "Downloading...",
								})
							}
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()

	// Remove from active downloads
	b.downloadsMutex.Lock()
	delete(b.activeDownloads, fileID)
	b.downloadsMutex.Unlock()

	if err != nil {
		// If the process was killed (cancelled), don't return an error
		if err.Error() == "signal: killed" || strings.Contains(err.Error(), "killed") {
			return nil // Cancellation is not an error
		}
		if stderrOutput != "" {
			return fmt.Errorf("om download-product (stemcell) failed: %s\n%s", err, stderrOutput)
		}
		return fmt.Errorf("om download-product (stemcell) failed: %w", err)
	}

	// Emit completion event
	runtime.EventsEmit(b.ctx, "download-complete", map[string]interface{}{
		"fileID": fileID,
		"path":   savePath,
	})

	return nil
}

// DownloadOpsManagerWithOM downloads an Ops Manager file using the OM CLI
func (b *BroadcomService) DownloadOpsManagerWithOM(productSlug string, releaseVersion string, fileName string, awsObjectKey string, savePath string, fileID int) error {
	if b.apiToken == "" {
		return fmt.Errorf("API token not set")
	}

	// Get the bundled OM CLI path
	omPath, err := GetOMPath()
	if err != nil {
		return fmt.Errorf("failed to get OM CLI: %w", err)
	}

	// savePath is now the output directory (not a full file path)
	outputDir := savePath
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Initial progress will come from OM CLI output

	// For Ops Manager, extract the IaaS from the filename
	// Example: "Tanzu Ops Manager for vSphere - 3.2.0"
	opsManagerIaas := "vsphere" // Default to vsphere
	lowerName := strings.ToLower(fileName)
	if strings.Contains(lowerName, "vsphere") {
		opsManagerIaas = "vsphere"
	} else if strings.Contains(lowerName, "aws") {
		opsManagerIaas = "aws"
	} else if strings.Contains(lowerName, "azure") {
		opsManagerIaas = "azure"
	} else if strings.Contains(lowerName, "gcp") || strings.Contains(lowerName, "google") {
		opsManagerIaas = "gcp"
	} else if strings.Contains(lowerName, "openstack") {
		opsManagerIaas = "openstack"
	}

	// Run om download-product with ops-manager-specific approach
	// Ops Manager uses a glob pattern based on the IaaS
	fileGlob := fmt.Sprintf("*%s*", opsManagerIaas)

	cmd := exec.Command(omPath,
		"download-product",
		"-t", b.apiToken,
		"-p", productSlug,
		"--product-version", releaseVersion,
		"-f", fileGlob,
		"-o", outputDir,
	)

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start om command: %w", err)
	}

	// Store in active downloads
	b.downloadsMutex.Lock()
	b.activeDownloads[fileID] = cmd
	b.downloadsMutex.Unlock()

	// Read stdout (not used for progress but consume it)
	go func() {
		buffer := make([]byte, 1024)
		for {
			_, err := stdout.Read(buffer)
			if err != nil {
				break
			}
		}
	}()

	// Read stderr and parse progress (handle carriage returns)
	var stderrOutput string
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				stderrOutput += chunk

				// Split by both newline and carriage return to handle progress updates
				parts := strings.FieldsFunc(chunk, func(r rune) bool {
					return r == '\n' || r == '\r'
				})

				for _, part := range parts {
					if part == "" || !strings.Contains(part, "%") {
						continue
					}

					// Pattern: " 211.14 MiB / 18.47 GiB [>------]   1.12% 02m56s"
					// Extract: total size and percentage
					reWithSize := regexp.MustCompile(`[\d.]+\s+([KMG]i?B)\s*/\s*([\d.]+)\s+([KMG]i?B)\s+\[.*?\]\s+([\d.]+)%`)
					matches := reWithSize.FindStringSubmatch(part)

					if len(matches) >= 5 {
						totalSizeStr := matches[2]
						unit := matches[3]
						percentage, _ := strconv.ParseFloat(matches[4], 64)
						totalSize, _ := strconv.ParseFloat(totalSizeStr, 64)

						var totalBytes int64
						switch unit {
						case "GiB", "GB":
							totalBytes = int64(totalSize * 1024 * 1024 * 1024)
						case "MiB", "MB":
							totalBytes = int64(totalSize * 1024 * 1024)
						case "KiB", "KB":
							totalBytes = int64(totalSize * 1024)
						}

						runtime.EventsEmit(b.ctx, "download-progress", map[string]interface{}{
							"fileID":    fileID,
							"progress":  percentage,
							"totalSize": totalBytes,
							"status":    "Downloading...",
						})
					} else {
						// Fallback: extract percentage only
						re := regexp.MustCompile(`([\d.]+)%`)
						matches := re.FindStringSubmatch(part)
						if len(matches) > 1 {
							if percentage, err := strconv.ParseFloat(matches[1], 64); err == nil {
								runtime.EventsEmit(b.ctx, "download-progress", map[string]interface{}{
									"fileID":   fileID,
									"progress": percentage,
									"status":   "Downloading...",
								})
							}
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()

	// Remove from active downloads
	b.downloadsMutex.Lock()
	delete(b.activeDownloads, fileID)
	b.downloadsMutex.Unlock()

	if err != nil {
		// If the process was killed (cancelled), don't return an error
		if err.Error() == "signal: killed" || strings.Contains(err.Error(), "killed") {
			return nil // Cancellation is not an error
		}
		if stderrOutput != "" {
			return fmt.Errorf("om download-product (ops-manager) failed: %s\n%s", err, stderrOutput)
		}
		return fmt.Errorf("om download-product (ops-manager) failed: %w", err)
	}

	// Emit completion event
	runtime.EventsEmit(b.ctx, "download-complete", map[string]interface{}{
		"fileID": fileID,
		"path":   savePath,
	})

	return nil
}

// DownloadFileWithOM downloads a product file using the OM CLI
func (b *BroadcomService) DownloadFileWithOM(productSlug string, releaseVersion string, fileName string, awsObjectKey string, savePath string, fileID int) error {
	if b.apiToken == "" {
		return fmt.Errorf("API token not set")
	}

	// Get the bundled OM CLI path
	omPath, err := GetOMPath()
	if err != nil {
		return fmt.Errorf("failed to get OM CLI: %w", err)
	}

	// savePath is now the output directory (not a full file path)
	outputDir := savePath
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Initial progress will come from OM CLI output

	// Run om download-product
	// For tiles, extract the actual filename from awsObjectKey if available
	// awsObjectKey format: "path/to/actual-file-name.pivotal"
	fileGlob := fileName
	if awsObjectKey != "" {
		// Extract just the filename from the aws object key path
		parts := strings.Split(awsObjectKey, "/")
		if len(parts) > 0 {
			actualFileName := parts[len(parts)-1]
			if actualFileName != "" {
				fileGlob = actualFileName
			}
		}
	} else if !strings.Contains(fileName, "*") && !strings.HasSuffix(fileName, ".pivotal") {
		// If no awsObjectKey and fileName is a display name, use a wildcard
		fileGlob = "*.pivotal"
	}

	cmd := exec.Command(omPath,
		"download-product",
		"-t", b.apiToken,
		"-p", productSlug,
		"--product-version", releaseVersion,
		"-f", fileGlob,
		"-o", outputDir,
	)

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start om command: %w", err)
	}

	// Store in active downloads
	b.downloadsMutex.Lock()
	b.activeDownloads[fileID] = cmd
	b.downloadsMutex.Unlock()

	// Read stdout (not used for progress but consume it)
	go func() {
		buffer := make([]byte, 1024)
		for {
			_, err := stdout.Read(buffer)
			if err != nil {
				break
			}
		}
	}()

	// Read stderr and parse progress (handle carriage returns)
	var stderrOutput string
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				stderrOutput += chunk

				// Split by both newline and carriage return to handle progress updates
				parts := strings.FieldsFunc(chunk, func(r rune) bool {
					return r == '\n' || r == '\r'
				})

				for _, part := range parts {
					if part == "" || !strings.Contains(part, "%") {
						continue
					}

					// Pattern: " 211.14 MiB / 18.47 GiB [>------]   1.12% 02m56s"
					// Extract: total size and percentage
					reWithSize := regexp.MustCompile(`[\d.]+\s+([KMG]i?B)\s*/\s*([\d.]+)\s+([KMG]i?B)\s+\[.*?\]\s+([\d.]+)%`)
					matches := reWithSize.FindStringSubmatch(part)

					if len(matches) >= 5 {
						totalSizeStr := matches[2]
						unit := matches[3]
						percentage, _ := strconv.ParseFloat(matches[4], 64)
						totalSize, _ := strconv.ParseFloat(totalSizeStr, 64)

						var totalBytes int64
						switch unit {
						case "GiB", "GB":
							totalBytes = int64(totalSize * 1024 * 1024 * 1024)
						case "MiB", "MB":
							totalBytes = int64(totalSize * 1024 * 1024)
						case "KiB", "KB":
							totalBytes = int64(totalSize * 1024)
						}

						runtime.EventsEmit(b.ctx, "download-progress", map[string]interface{}{
							"fileID":    fileID,
							"progress":  percentage,
							"totalSize": totalBytes,
							"status":    "Downloading...",
						})
					} else {
						// Fallback: extract percentage only
						re := regexp.MustCompile(`([\d.]+)%`)
						matches := re.FindStringSubmatch(part)
						if len(matches) > 1 {
							if percentage, err := strconv.ParseFloat(matches[1], 64); err == nil {
								runtime.EventsEmit(b.ctx, "download-progress", map[string]interface{}{
									"fileID":   fileID,
									"progress": percentage,
									"status":   "Downloading...",
								})
							}
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()

	// Remove from active downloads and check if it was cancelled
	b.downloadsMutex.Lock()
	delete(b.activeDownloads, fileID)
	b.downloadsMutex.Unlock()

	if err != nil {
		// If the process was killed (cancelled), don't emit completion or error
		if err.Error() == "signal: killed" || strings.Contains(err.Error(), "killed") {
			// Don't emit anything - cancellation event was already sent
			return nil
		}
		if stderrOutput != "" {
			return fmt.Errorf("om download-product failed: %s\n%s", err, stderrOutput)
		}
		return fmt.Errorf("om download-product failed: %w", err)
	}

	// Emit completion event only if not cancelled
	runtime.EventsEmit(b.ctx, "download-complete", map[string]interface{}{
		"fileID": fileID,
		"path":   savePath,
	})

	return nil
}

// GetReleaseDependencies retrieves all dependencies for a specific release
func (b *BroadcomService) GetReleaseDependencies(productSlug string, releaseID int) ([]Dependency, error) {
	if b.apiToken == "" {
		return nil, fmt.Errorf("API token not set")
	}

	url := fmt.Sprintf("%s/api/v2/products/%s/releases/%d/dependencies", b.baseURL, productSlug, releaseID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Dependencies []Dependency `json:"dependencies"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Dependencies, nil
}

// GetReleaseDependencySpecifiers retrieves dependency version specifiers for a specific release
func (b *BroadcomService) GetReleaseDependencySpecifiers(productSlug string, releaseID int) ([]DependencySpecifier, error) {
	if b.apiToken == "" {
		return nil, fmt.Errorf("API token not set")
	}

	url := fmt.Sprintf("%s/api/v2/products/%s/releases/%d/dependency_specifiers", b.baseURL, productSlug, releaseID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.apiToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		DependencySpecifiers []DependencySpecifier `json:"dependency_specifiers"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.DependencySpecifiers, nil
}

// AcceptEULAAndDownload accepts the EULA and downloads a product file with progress tracking
func (b *BroadcomService) AcceptEULAAndDownload(productSlug string, releaseID int, fileID int, savePath string) error {
	if b.apiToken == "" {
		return fmt.Errorf("API token not set")
	}

	// Ensure the download directory exists
	if err := os.MkdirAll(savePath, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	// Get the release version and file name first
	releases, err := b.GetProductReleases(productSlug)
	if err != nil {
		return fmt.Errorf("failed to get releases: %w", err)
	}

	var releaseVersion string
	for _, rel := range releases {
		if rel.ID == releaseID {
			releaseVersion = rel.Version
			break
		}
	}

	if releaseVersion == "" {
		return fmt.Errorf("could not find release version for ID %d", releaseID)
	}

	// Get file details
	files, err := b.GetReleaseFiles(productSlug, releaseID)
	if err != nil {
		return fmt.Errorf("failed to get files: %w", err)
	}

	var fileName string
	var awsObjectKey string
	for _, file := range files {
		if file.ID == fileID {
			fileName = file.Name
			awsObjectKey = file.AWSObjectKey
			break
		}
	}

	if fileName == "" {
		return fmt.Errorf("could not find file name for ID %d", fileID)
	}

	// Check if this is a stemcell product (different download command)
	// Stemcells have 'stemcell' in the product slug
	isStemcell := strings.Contains(strings.ToLower(productSlug), "stemcell")

	if isStemcell {
		return b.DownloadStemcellWithOM(productSlug, releaseVersion, fileName, awsObjectKey, savePath, fileID)
	}

	// Check if this is an Ops Manager product
	// Ops Manager products have 'ops-manager' in the slug or file type
	isOpsManager := strings.Contains(strings.ToLower(productSlug), "ops-manager")

	if isOpsManager {
		return b.DownloadOpsManagerWithOM(productSlug, releaseVersion, fileName, awsObjectKey, savePath, fileID)
	}

	// Use OM CLI to download regular products (tiles)
	return b.DownloadFileWithOM(productSlug, releaseVersion, fileName, awsObjectKey, savePath, fileID)
}
