package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AIModelService handles AI model downloads and packaging
type AIModelService struct {
	ctx                context.Context
	downloadLocation   string
	activeDownloads    map[string]*exec.Cmd
	downloadsMutex     sync.Mutex
	cancelChannels     map[string]chan bool
	cancelChannelMutex sync.Mutex
}

// ModelType represents the type of AI model
type ModelType string

const (
	ModelTypeOllama ModelType = "ollama"
	ModelTypeVLLM   ModelType = "vllm"
)

// NewAIModelService creates a new AI model service
func NewAIModelService() *AIModelService {
	return &AIModelService{
		activeDownloads: make(map[string]*exec.Cmd),
		cancelChannels:  make(map[string]chan bool),
	}
}

// startup initializes the service with context
func (a *AIModelService) startup(ctx context.Context) {
	a.ctx = ctx
}

// SetDownloadLocation sets the download location for AI models
func (a *AIModelService) SetDownloadLocation(location string) {
	a.downloadLocation = location
}

// getHuggingFaceCLI returns the correct CLI command based on platform
// macOS uses 'hf' (installed via brew), others use 'huggingface-cli' (installed via pip)
func getHuggingFaceCLI() (string, error) {
	// Check for 'hf' first (macOS brew installation)
	if _, err := exec.LookPath("hf"); err == nil {
		return "hf", nil
	}

	// Check for 'huggingface-cli' (pip installation)
	if _, err := exec.LookPath("huggingface-cli"); err == nil {
		return "huggingface-cli", nil
	}

	// Return appropriate error message based on platform
	if runtime.GOOS == "darwin" {
		return "", fmt.Errorf("HuggingFace CLI not found. Please install: brew install huggingface-cli")
	}
	return "", fmt.Errorf("huggingface-cli not found. Please install: pip install huggingface-hub[cli]")
}

// DownloadOllamaModel downloads GGUF files from HuggingFace
func (a *AIModelService) DownloadOllamaModel(repoURL string, modelName string) error {
	if a.downloadLocation == "" {
		return fmt.Errorf("download location not set")
	}

	// Parse HuggingFace URL to get repo and path
	// Example: https://huggingface.co/unsloth/Llama-3.3-70B-Instruct-GGUF/tree/main/UD-Q6_K_XL
	parts := strings.Split(strings.TrimPrefix(repoURL, "https://huggingface.co/"), "/")
	if len(parts) < 4 {
		return fmt.Errorf("invalid HuggingFace URL format")
	}

	owner := parts[0]
	repo := parts[1]
	// Skip "tree/main" or "tree/master"
	subPath := strings.Join(parts[4:], "/")

	// Create model directory
	modelDir := filepath.Join(a.downloadLocation, modelName)
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
		"modelName": modelName,
		"status":    "Starting download...",
		"progress":  10,
	})

	// Use huggingface-cli to download the files
	return a.downloadWithHuggingFaceCLI(owner+"/"+repo, subPath, modelDir, modelName, "*.gguf")
}

// DownloadVLLMModel downloads safetensors and config files, then packages as tar.gz
func (a *AIModelService) DownloadVLLMModel(repoURL string, modelName string) error {
	if a.downloadLocation == "" {
		return fmt.Errorf("download location not set")
	}

	// Parse HuggingFace URL
	// Example: https://huggingface.co/openai/gpt-oss-120b
	// or: https://huggingface.co/openai/gpt-oss-120b/tree/main
	repoPath := strings.TrimPrefix(repoURL, "https://huggingface.co/")
	repoPath = strings.TrimSuffix(repoPath, "/")

	// Remove /tree/main or /tree/master if present (vLLM downloads from root)
	if idx := strings.Index(repoPath, "/tree/"); idx != -1 {
		repoPath = repoPath[:idx]
	}

	// Create temp directory for downloads (visible in Downloads folder)
	tempDir := filepath.Join(a.downloadLocation, modelName+"_temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory

	wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
		"modelName": modelName,
		"status":    "Downloading model files...",
		"progress":  10,
	})

	// Download using huggingface-cli
	// First, download .safetensors files
	if err := a.downloadVLLMFiles(repoPath, tempDir, modelName); err != nil {
		// Check if this was a cancellation (not an error)
		if strings.Contains(err.Error(), "cancelled") {
			// Don't return error for cancellation - it's expected behavior
			return nil
		}
		return err
	}

	wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
		"modelName": modelName,
		"status":    "Packaging model...",
		"progress":  80,
	})

	// Package as tar.gz with files at root level
	// Get the cancel channel for this download
	a.cancelChannelMutex.Lock()
	cancelChan, exists := a.cancelChannels[modelName]
	a.cancelChannelMutex.Unlock()

	tarGzPath := filepath.Join(a.downloadLocation, modelName+".tar.gz")
	if err := a.packageVLLMModel(tempDir, tarGzPath, modelName, cancelChan); err != nil {
		if exists {
			a.cancelChannelMutex.Lock()
			delete(a.cancelChannels, modelName)
			a.cancelChannelMutex.Unlock()
		}
		// Check if this was a cancellation (not an error)
		if strings.Contains(err.Error(), "cancelled") {
			// Don't return error for cancellation - it's expected behavior
			return nil
		}
		return err
	}

	wailsruntime.EventsEmit(a.ctx, "ai-model-complete", map[string]interface{}{
		"modelName": modelName,
		"path":      tarGzPath,
	})

	return nil
}

// downloadWithHuggingFaceCLI uses huggingface-cli to download files
func (a *AIModelService) downloadWithHuggingFaceCLI(repo string, pattern string, destDir string, modelName string, filePattern string) error {
	// Get the correct CLI command for the platform
	cliCmd, err := getHuggingFaceCLI()
	if err != nil {
		return err
	}

	// Create cancel channel for this download
	cancelChan := make(chan bool, 1)
	a.cancelChannelMutex.Lock()
	a.cancelChannels[modelName] = cancelChan
	a.cancelChannelMutex.Unlock()

	// Download with huggingface-cli or hf
	// Use --local-dir-use-symlinks=False to download directly to target folder
	args := []string{
		"download",
		repo,
		"--include", pattern + "/*",
		"--local-dir", destDir,
		"--local-dir-use-symlinks", "False",
	}

	cmd := exec.Command(cliCmd, args...)

	// Capture output for debugging
	var stderr strings.Builder
	cmd.Stderr = &stderr

	// Store in active downloads
	a.downloadsMutex.Lock()
	a.activeDownloads[modelName] = cmd
	a.downloadsMutex.Unlock()

	// Emit status before starting
	wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
		"modelName": modelName,
		"status":    "Downloading files from HuggingFace...",
		"progress":  20,
	})

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start download: %w", err)
	}

	// Monitor progress by checking directory size
	progressDone := make(chan bool)
	go a.monitorDownloadProgress(destDir, modelName, cancelChan, progressDone)

	// Wait for completion or cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-cancelChan:
		close(progressDone)
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("download cancelled")
	case err := <-done:
		close(progressDone)

		a.downloadsMutex.Lock()
		delete(a.activeDownloads, modelName)
		a.downloadsMutex.Unlock()

		a.cancelChannelMutex.Lock()
		delete(a.cancelChannels, modelName)
		a.cancelChannelMutex.Unlock()

		if err != nil {
			errMsg := stderr.String()
			if errMsg != "" {
				return fmt.Errorf("download failed: %w - %s", err, errMsg)
			}
			return fmt.Errorf("download failed: %w", err)
		}
	}

	// After download completes, concatenate GGUF files if this is an Ollama model
	if filePattern == "*.gguf" {
		wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
			"modelName": modelName,
			"status":    "Concatenating GGUF files...",
			"progress":  90,
		})

		if err := a.concatenateGGUFFiles(destDir, modelName); err != nil {
			return fmt.Errorf("failed to concatenate GGUF files: %w", err)
		}
	}

	wailsruntime.EventsEmit(a.ctx, "ai-model-complete", map[string]interface{}{
		"modelName": modelName,
		"path":      destDir,
	})

	return nil
}

// downloadVLLMFiles downloads safetensors and config files for vLLM
func (a *AIModelService) downloadVLLMFiles(repo string, destDir string, modelName string) error {
	// Get the correct CLI command for the platform
	cliCmd, err := getHuggingFaceCLI()
	if err != nil {
		return err
	}

	// Create cancel channel
	cancelChan := make(chan bool, 1)
	a.cancelChannelMutex.Lock()
	a.cancelChannels[modelName] = cancelChan
	a.cancelChannelMutex.Unlock()

	// Download safetensors, json, and jinja files (root level only)
	// Exclude ALL subdirectories - only get files from repository root
	args := []string{
		"download",
		repo,
		"--include", "*.safetensors",
		"--include", "*.json",
		"--include", "*.jinja",
		"--exclude", "*/*",  // Exclude all files in subdirectories
		"--local-dir", destDir,
	}

	cmd := exec.Command(cliCmd, args...)

	// Capture output for debugging
	var stderr strings.Builder
	cmd.Stderr = &stderr

	a.downloadsMutex.Lock()
	a.activeDownloads[modelName] = cmd
	a.downloadsMutex.Unlock()

	// Emit status before starting
	wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
		"modelName": modelName,
		"status":    "Downloading model files from HuggingFace...",
		"progress":  30,
	})

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start download: %w", err)
	}

	// Monitor progress by checking directory size
	progressDone := make(chan bool)
	go a.monitorDownloadProgress(destDir, modelName, cancelChan, progressDone)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-cancelChan:
		close(progressDone)
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("download cancelled")
	case err := <-done:
		close(progressDone)
		a.downloadsMutex.Lock()
		delete(a.activeDownloads, modelName)
		a.downloadsMutex.Unlock()

		if err != nil {
			errMsg := stderr.String()
			if errMsg != "" {
				return fmt.Errorf("download failed: %w - %s", err, errMsg)
			}
			return fmt.Errorf("download failed: %w", err)
		}
	}

	return nil
}

// packageVLLMModel creates a tar.gz with files at root level
func (a *AIModelService) packageVLLMModel(sourceDir string, tarGzPath string, modelName string, cancelChan chan bool) error {
	// Create the tar.gz file
	outFile, err := os.Create(tarGzPath)
	if err != nil {
		return fmt.Errorf("failed to create tar.gz: %w", err)
	}
	defer outFile.Close()

	// Use fastest compression level (BestSpeed) for better performance
	// Model files (safetensors) are already compressed and won't benefit from high compression
	gzipWriter, err := gzip.NewWriterLevel(outFile, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Walk through source directory and add files to tar
	// Files MUST be at root level (no subdirectories)
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check for cancellation (handle nil channel)
		if cancelChan != nil {
			select {
			case <-cancelChan:
				return fmt.Errorf("packaging cancelled")
			default:
			}
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only include files directly in sourceDir (not in subdirectories)
		if filepath.Dir(path) != sourceDir {
			return nil
		}

		// Skip hidden files and .git directories
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Open the file
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Create tar header with file at root level (no path prefix)
		header := &tar.Header{
			Name:    info.Name(), // Just the filename, no path
			Size:    info.Size(),
			Mode:    int64(info.Mode()),
			ModTime: info.ModTime(),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// Copy file content with periodic cancellation checks
		// Use a buffer to check for cancellation every 32MB
		buf := make([]byte, 32*1024*1024)
		for {
			// Check for cancellation before each read
			if cancelChan != nil {
				select {
				case <-cancelChan:
					return fmt.Errorf("packaging cancelled")
				default:
				}
			}

			n, err := file.Read(buf)
			if n > 0 {
				if _, writeErr := tarWriter.Write(buf[:n]); writeErr != nil {
					return writeErr
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to package model: %w", err)
	}

	return nil
}

// monitorDownloadProgress monitors the download directory and emits progress updates
func (a *AIModelService) monitorDownloadProgress(destDir string, modelName string, cancelChan chan bool, done chan bool) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastSize := int64(0)
	noChangeTicks := 0
	const maxNoChangeTicks = 3 // If no change for 3 seconds, might be between files

	for {
		select {
		case <-done:
			return
		case <-cancelChan:
			return
		case <-ticker.C:
			// Calculate directory size
			currentSize := getDirSize(destDir)
			sizeGB := float64(currentSize) / (1024 * 1024 * 1024)

			// Check if size is changing
			if currentSize > lastSize {
				// Size is growing - calculate progress dynamically
				// Use logarithmic scale for progress: 20% + (log growth)
				// This provides smoother progress without knowing total size
				sizeMB := float64(currentSize) / (1024 * 1024)

				// Progress based on downloaded size (rough estimate)
				// 100MB = ~25%, 500MB = ~40%, 1GB = ~50%, 5GB = ~65%, 10GB+ = ~80%
				var progress int
				if sizeMB < 100 {
					progress = 20 + int(sizeMB/100*5) // 20-25%
				} else if sizeMB < 500 {
					progress = 25 + int((sizeMB-100)/400*15) // 25-40%
				} else if sizeMB < 1024 {
					progress = 40 + int((sizeMB-500)/524*10) // 40-50%
				} else if sizeMB < 5120 {
					progress = 50 + int((sizeMB-1024)/4096*15) // 50-65%
				} else if sizeMB < 10240 {
					progress = 65 + int((sizeMB-5120)/5120*15) // 65-80%
				} else {
					progress = 80 + int((sizeMB-10240)/10240*5) // 80-85% (cap)
					if progress > 85 {
						progress = 85
					}
				}

				wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
					"modelName": modelName,
					"status":    fmt.Sprintf("Downloading... (%.2f GB)", sizeGB),
					"progress":  progress,
				})

				lastSize = currentSize
				noChangeTicks = 0
			} else if currentSize == lastSize && currentSize > 0 {
				// No change but files exist - might be processing between files
				noChangeTicks++
				if noChangeTicks <= maxNoChangeTicks {
					wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
						"modelName": modelName,
						"status":    fmt.Sprintf("Processing... (%.2f GB)", sizeGB),
						"progress":  -1, // Keep current progress
					})
				}
			}
		}
	}
}

// getDirSize calculates the total size of all files in a directory
func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// concatenateGGUFFiles finds all GGUF part files and concatenates them into a single file
func (a *AIModelService) concatenateGGUFFiles(destDir string, modelName string) error {
	// Find all .gguf files in the directory
	var ggufFiles []string
	err := filepath.Walk(destDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".gguf") {
			ggufFiles = append(ggufFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to find GGUF files: %w", err)
	}

	if len(ggufFiles) == 0 {
		return fmt.Errorf("no GGUF files found in %s", destDir)
	}

	// If there's only one file, we're done (no need to concatenate)
	if len(ggufFiles) == 1 {
		return nil
	}

	// Sort files alphabetically to ensure correct order
	sort.Strings(ggufFiles)

	// Create output file
	outputPath := filepath.Join(destDir, modelName+".gguf")
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	// Concatenate all part files
	for i, partFile := range ggufFiles {
		wailsruntime.EventsEmit(a.ctx, "ai-model-status", map[string]interface{}{
			"modelName": modelName,
			"status":    fmt.Sprintf("Concatenating file %d of %d...", i+1, len(ggufFiles)),
			"progress":  90 + (i * 5 / len(ggufFiles)),
		})

		partData, err := os.Open(partFile)
		if err != nil {
			return fmt.Errorf("failed to open part file %s: %w", partFile, err)
		}

		if _, err := io.Copy(outputFile, partData); err != nil {
			partData.Close()
			return fmt.Errorf("failed to concatenate file %s: %w", partFile, err)
		}
		partData.Close()

		// Delete the part file after concatenation
		os.Remove(partFile)
	}

	return nil
}

// CancelModelDownload cancels an active model download
func (a *AIModelService) CancelModelDownload(modelName string) error {
	a.cancelChannelMutex.Lock()
	cancelChan, exists := a.cancelChannels[modelName]
	a.cancelChannelMutex.Unlock()

	if !exists {
		return fmt.Errorf("no active download found for model: %s", modelName)
	}

	// Signal cancellation
	cancelChan <- true

	wailsruntime.EventsEmit(a.ctx, "ai-model-cancelled", map[string]interface{}{
		"modelName": modelName,
	})

	return nil
}
