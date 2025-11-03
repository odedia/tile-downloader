# Embedded OM CLI Binaries

This directory contains the OM CLI binaries that are embedded into the Tanzu Downloader application.

## Important Note

⚠️ **The `bin/` directory is NOT checked into git** to keep the repository size small (~300MB of binaries).

The OM CLI binaries are automatically downloaded during the build process by GitHub Actions and by developers running the build script locally.

## License

The OM CLI is licensed under the Apache License 2.0. See: https://github.com/pivotal-cf/om/blob/main/LICENSE

## For Developers

To build locally, download the OM CLI binaries first:

```bash
./download-om-binaries.sh
```

Or manually download from: https://github.com/pivotal-cf/om/releases/latest

## Current Version

Version: 7.18.2 (automatically downloaded during build)

## Binaries Downloaded

The build process downloads:
- `bin/om-darwin-amd64` - macOS Intel
- `bin/om-darwin-arm64` - macOS Apple Silicon
- `bin/om-linux-amd64` - Linux AMD64
- `bin/om-windows-amd64.exe` - Windows AMD64

## For CI/CD

GitHub Actions automatically runs `download-om-binaries.sh` during the build process, so no pre-downloaded binaries are needed in the repository.
