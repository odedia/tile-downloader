# Tanzu Download Manager

A desktop application for downloading VMware Tanzu products from the Broadcom Support Portal (formerly Tanzu Network).

## ⚠️ Disclaimer

This is a community project and is NOT affiliated with, endorsed by, or supported by Broadcom or VMware Tanzu. This software is provided as-is with no warranty or official support of any kind. Use at your own risk.

## Features

- **Browse Tanzu Products**: View all available VMware Tanzu products and releases
- **Smart Filtering**: Filter products to show only Tanzu Platform downloads
- **Product Sorting**: Automatically prioritizes key products like Foundation Core and Elastic Application Runtime
- **File Categorization**: Automatically identifies and categorizes Tiles, Stemcells, and Ops Manager files
- **Progress Tracking**: Real-time download progress with file size information
- **Active Downloads**: Dedicated page to monitor all active and completed downloads
- **Download Management**: Cancel downloads with support for OM CLI features
- **EULA Management**: Automatic EULA acceptance before downloading
- **AI Model Packager**: Download and package AI models from HuggingFace for Tanzu Platform AI Services
  - Support for vLLM models (safetensors format)
  - Support for Ollama models (GGUF format)
  - Automatic packaging as tar.gz for easy deployment
  - Real-time download progress with size tracking
- **Download Planner**: Plan and download complete TAS environments with compatible versions
- **Settings**: Configure download location and API token

## Technology Stack

- **Backend**: Go (Golang)
- **Frontend**: Svelte
- **Framework**: Wails v2 - Build desktop apps using Go & Web Technologies
- **Download Tool**: OM CLI for authenticated downloads from Broadcom Support Portal

## Prerequisites

For end users:
- **Broadcom Support Portal API Token** (only requirement!)

For developers:
- [Go](https://golang.org/dl/) 1.18 or later
- [Node.js](https://nodejs.org/) 16 or later
- [Wails](https://wails.io/) v2 framework

**Note**: The OM CLI is bundled inside the application - no separate installation needed!

## Installation

### From Binary Release

1. Download the latest release for your platform from the [Releases page](https://github.com/odedia/tanzu-downloader/releases)
2. Extract the archive
3. Run the application (no additional dependencies required!)

### From Source

1. Clone the repository:
```bash
git clone https://github.com/odedia/tanzu-downloader.git
cd tanzu-downloader
```

2. Install Wails:
```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

3. Download OM CLI binaries (for embedding):
```bash
./download-om-binaries.sh
```

4. Build the application:
```bash
wails build
```

5. Run the application:
```bash
# On macOS
./build/bin/tanzu-downloader.app/Contents/MacOS/tanzu-downloader

# On Linux
./build/bin/tanzu-downloader

# On Windows
./build/bin/tanzu-downloader.exe
```

### For Development

**Important**: Download OM CLI binaries before first run:

```bash
./download-om-binaries.sh
```

Then start the dev server:

```bash
wails dev
```

## Getting Your API Token

1. Visit the [Broadcom Support Portal](https://support.broadcom.com/)
2. Log in with your credentials
3. Navigate to your profile settings
4. Generate an API token
5. Copy the token and paste it into the Tanzu Download Manager settings

## Usage

1. **Configure API Token**: Click "Change API Token" and enter your Broadcom Support Portal API token
2. **Browse Products**: View all available Tanzu products on the main page
3. **Filter Products**: Enable "Only show Tanzu Platform downloads" to focus on Tanzu Platform products
4. **Select Product**: Click on a product card to view available releases
5. **Choose Release**: Select a release version to see downloadable files
6. **Download Files**: Click the "Download" button next to any file
7. **Accept EULA**: Review and accept the EULA when prompted
8. **Monitor Progress**: View real-time download progress with percentage and file size
9. **Active Downloads**: Click "Active Downloads" to see all ongoing and completed downloads
10. **Cancel Downloads**: Use the "Cancel" button to stop unwanted downloads

## File Types

The application automatically categorizes files:

- **Tiles** (Purple badge): Product tiles with `.pivotal` extension
- **Stemcells** (Orange badge): BOSH stemcells for various IaaS platforms
- **Ops Manager** (Green badge): VMware Tanzu Operations Manager images

## Configuration

Settings are stored locally and include:
- API Token
- Download Location
- Product Filter Preferences

## Project Structure

```
tanzu-downloader/
├── main.go                    # Application entry point
├── broadcom.go                # Broadcom API service
├── omcli.go                   # OM CLI embedding logic
├── app.go                     # Application struct
├── embed/                     # Embedded binaries
│   ├── bin/                   # OM CLI binaries for all platforms
│   └── README.md
├── download-om-binaries.sh    # Script to download OM CLI
├── frontend/                  # Svelte frontend
│   ├── src/
│   │   └── App.svelte         # Main UI component
│   └── package.json
├── build/                     # Build output
└── wails.json                 # Wails configuration
```

## Embedded OM CLI

This application embeds the OM CLI for all supported platforms, providing a truly portable single-binary experience. On first run, the appropriate OM CLI binary is extracted to a temporary directory and used for all downloads. This means:

- ✅ No external dependencies required
- ✅ Works offline (after initial setup)
- ✅ Consistent experience across all platforms
- ✅ Automatic platform detection (macOS Intel/ARM, Linux, Windows)

The OM CLI is licensed under Apache 2.0 and bundled in compliance with its license terms.

## Releases

This project uses GitHub Actions for automated multi-platform builds and releases.

### Creating a Release

1. Ensure all changes are committed and pushed to `main`
2. Create and push a version tag:
```bash
git tag v1.0.0
git push origin v1.0.0
```

3. GitHub Actions will automatically:
   - Build binaries for all platforms (macOS Intel/ARM, Windows x64, Linux x64)
   - Create a GitHub Release
   - Upload all artifacts

### Available Platforms

Releases are automatically built for:
- **macOS Intel (x64)** - `tanzu-downloader-macos-intel.zip`
- **macOS Apple Silicon (ARM64)** - `tanzu-downloader-macos-arm64.zip`
- **Windows x64** - `tanzu-downloader-windows-x64.zip`
- **Linux x64** - `tanzu-downloader-linux-x64.tar.gz`

See [.github/workflows/README.md](.github/workflows/README.md) for detailed workflow documentation.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Development Workflow

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test builds locally: `wails build`
5. Submit a Pull Request

The build test workflow will automatically validate your changes on all platforms.

## License

Apache License 2.0 - see LICENSE file for details

## Acknowledgments

- Built with [Wails](https://wails.io/)
- Uses [OM CLI](https://github.com/pivotal-cf/om) for downloads
- Designed for the VMware Tanzu community

## Support

For issues, questions, or contributions, please visit the [GitHub repository](https://github.com/odedia/tanzu-downloader).
