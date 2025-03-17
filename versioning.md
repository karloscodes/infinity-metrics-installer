# Infinity Metrics Versioning Strategy

This document explains how versioning works for the `infinity-metrics-installer` project in a simple way. It covers how versions are defined, how releases are created, and how the `config.json` file fits into the process to keep your application up-to-date.

## Key Concepts

- **Version File**: A `.version` file in the repository stores the current version number (e.g., `1.0.0`).
- **Automatic Releases**: Every commit to the `main` branch triggers a new release of the binary using the version from `.version`.
- **Config File**: A `config.json` file in the repository specifies settings and is included in each release.

## How It Works

### 1. Version in `.version`
- The `.version` file contains a single line with the current version, like `1.0.0`.
- You update this file manually when you want a new version (e.g., `1.0.1` for a small update or `2.0.0` for a big change).

### 2. Automatic Release Creation
- When you push a commit to the `main` branch:
  - A GitHub Action (or similar CI/CD tool) reads the version from `.version`.
  - It builds the `infinity-metrics` binary for different architectures (e.g., `infinity-metrics-v1.0.0-amd64` for Intel CPUs, `infinity-metrics-v1.0.0-arm64` for ARM CPUs).
  - It also includes the `config.json` file from the repository.
  - It creates a new GitHub release with a tag like `v1.0.0` and attaches these files as assets.
- This happens automatically for every commit to `main`, so each change gets a new release with the same version until `.version` is updated.

### 3. The `config.json` File
- The `config.json` file in the repository looks like this:
```json
  {
    "app_image": "karloscodes/infinity-metrics-beta:latest",
    "caddy_image": "caddy:2.7-alpine",
    "version": "latest"
  }
```
- **Purpose**: 
  - `"app_image"`: Specifies the Docker image for the Infinity Metrics application (e.g., `karloscodes/infinity-metrics-beta:latest`).
  - `"caddy_image"`: Specifies the Docker image for the Caddy web server (e.g., `caddy:2.7-alpine`).
  - `"version"`: Set to `"latest"`, indicating the system should always use the most recent release available.
- **How It’s Used**:
  - When a release is created, this `config.json` is uploaded as an asset alongside the binaries.
  - The application fetches this file from the latest GitHub release to determine which Docker images to use.
  - The `"version": "latest"` field reinforces that the system should rely on the most current release, though the actual version comes from `.version` and the release tag (e.g., `v1.0.0`).

### 4. Keeping It Updated
- The application uses the version from the release tag (e.g., `1.0.0`) to check if a newer binary is available.
- It downloads the latest binary and `config.json` from the GitHub release when needed.
- The `config.json` ensures the correct Docker images are used, staying consistent with the "always latest" approach.

## Example Flow
1. You set `.version` to `1.0.0` and push a commit to `main`.
2. A release `v1.0.0` is created with binaries (e.g., `infinity-metrics-v1.0.0-amd64`) and `config.json`.
3. The application starts with `v1.0.0` and uses `config.json` to pull `karloscodes/infinity-metrics-beta:latest` and `caddy:2.7-alpine`.
4. Later, you update `.version` to `1.0.1` and push another commit.
5. A new release `v1.0.1` is created with updated binaries and the same `config.json`.
6. The application detects `1.0.1`, downloads it, and uses the same `config.json` settings.

## Why It’s Simple
- **One Version Source**: `.version` controls the release version.
- **Automatic Releases**: Every `main` commit triggers a release, no manual tagging needed.
- **Config Consistency**: `config.json` is released with each version, ensuring Docker images stay up-to-date without extra configuration.

## Notes
- If you don’t update `.version`, new commits create releases with the same version (e.g., `v1.0.0`), and the application only updates if the version number increases.
- The `config.json` file’s `"version": "latest"` doesn’t override the `.version` file —it’s a signal to always fetch the latest release available.

This strategy keeps versioning straightforward and automated, with `config.json` providing a consistent setup for each release!
