# GitHub Actions Workflows

This directory contains GitHub Actions workflows for building and releasing the Tile Downloader.

## Workflows

### 1. Build Test (`build.yml`)

**Triggers:**
- On pull requests to `main`
- On pushes to `main` branch

**Purpose:**
- Validates that the application builds successfully on all platforms
- Runs on: macOS (Universal), Windows x64, Linux x64
- Does not create releases or artifacts

**Use case:** Continuous integration testing to catch build issues early

---

### 2. Build and Release (`release.yml`)

**Triggers:**
- When a git tag matching `v*` is pushed (e.g., `v1.0.0`)
- Manual workflow dispatch

**Purpose:**
- Builds production-ready binaries for all platforms
- Creates a GitHub Release
- Uploads release artifacts

**Platforms:**
- macOS Intel (darwin/amd64)
- macOS Apple Silicon (darwin/arm64)
- Windows x64 (windows/amd64)
- Linux x64 (linux/amd64)

**Artifacts:**
- `tanzu-downloader-macos-intel.zip`
- `tanzu-downloader-macos-arm64.zip`
- `tanzu-downloader-windows-x64.zip`
- `tanzu-downloader-linux-x64.tar.gz`

---

## Creating a Release

### Automatic Release (Recommended)

1. Ensure all changes are committed and pushed to `main`
2. Create and push a version tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```

3. The workflow will automatically:
   - Build binaries for all platforms
   - Create a GitHub Release
   - Upload all artifacts

### Manual Release

1. Go to the [Actions tab](../../actions)
2. Select "Build and Release" workflow
3. Click "Run workflow"
4. Enter the version (e.g., `v1.0.0`)
5. Click "Run workflow"

---

## Workflow Requirements

### Secrets
- `GITHUB_TOKEN` - Automatically provided by GitHub Actions

### OM CLI Binaries
- The workflow automatically downloads OM CLI binaries using `download-om-binaries.sh`
- No pre-downloaded binaries needed in the repository

### Build Times
Approximate build times:
- macOS: 5-7 minutes
- Windows: 4-6 minutes
- Linux: 3-5 minutes
- Total release time: ~15-20 minutes

---

## Troubleshooting

### Build Fails on macOS
- Ensure Xcode Command Line Tools are properly configured
- Check Go and Node.js versions

### Build Fails on Linux
- Verify GTK3 and WebKit2GTK dependencies are installed
- Check the dependency installation step

### Build Fails on Windows
- Ensure proper Windows SDK is available
- Check Go installation on Windows runner

### Release Not Created
- Verify the tag follows the `v*` pattern (e.g., `v1.0.0`, not `1.0.0`)
- Check that the workflow has `contents: write` permission
- Ensure artifacts were successfully uploaded

---

## Local Testing

Before pushing a release tag, test the build locally:

```bash
# Download OM CLI binaries
./download-om-binaries.sh

# Test build
wails build -platform darwin/universal  # macOS
wails build -platform windows/amd64    # Windows
wails build -platform linux/amd64      # Linux
```

---

## Modifying Workflows

### Adding a New Platform

1. Add a new matrix entry in `release.yml`:
```yaml
- name: Linux-ARM64
  os: ubuntu-latest
  platform: linux/arm64
  output_name: tanzu-downloader-linux-arm64
  artifact_path: build/bin/tanzu-downloader
```

2. Update the release body and files list

### Changing Build Configuration

- Modify the `wails build` command flags
- Add/remove build steps as needed
- Update Go or Node.js versions in setup steps

---

## Notes

- Builds include the embedded OM CLI (~300MB of binaries)
- Final artifacts are compressed (zip for Windows/macOS, tar.gz for Linux)
- macOS builds may require code signing for distribution outside GitHub
