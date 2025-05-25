package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// GetLocalImageDigest returns the digest of a local image if it exists
func (d *Docker) GetLocalImageDigest(image string) (string, error) {
	// We don't need a context here since RunCommand doesn't support it
	// But we'll add a timeout check for consistency in logging
	start := time.Now()
	defer func() {
		if time.Since(start) > 5*time.Second {
			d.logger.Warn("GetLocalImageDigest took longer than expected: %v", time.Since(start))
		}
	}()

	// First check if the image exists locally
	output, err := d.RunCommand("images", "--format", "{{.Repository}}:{{.Tag}}", image)
	if err != nil || strings.TrimSpace(output) == "" {
		d.logger.Debug("Image %s not found locally", image)
		return "", fmt.Errorf("image not found locally: %s", image)
	}

	// Get the image ID (which is the digest in Docker's internal format)
	output, err = d.RunCommand("inspect", "--format", "{{.Id}}", image)
	if err != nil {
		return "", fmt.Errorf("failed to inspect local image: %w", err)
	}
	
	digest := strings.TrimSpace(output)
	if digest == "" {
		return "", fmt.Errorf("empty digest returned for local image: %s", image)
	}
	
	// Normalize the digest format to be comparable with remote digests
	// Sometimes Docker prefixes the digest with 'sha256:'
	if !strings.Contains(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	
	d.logger.Debug("Local digest for %s: %s", image, digest)
	return digest, nil
}

// Cache structure to store image digests with expiration
type digestCacheEntry struct {
	digest    string
	expiresAt time.Time
}

// Cache to store image digests
var (
	digestCache     = make(map[string]digestCacheEntry)
	digestCacheMux  sync.RWMutex
	digestCacheTTL  = 5 * time.Minute // Cache entries expire after 5 minutes
)

// GetRemoteImageDigest fetches the digest of a remote image without pulling it
// Uses go-containerregistry to properly handle multi-architecture images
func (d *Docker) GetRemoteImageDigest(image string) (string, error) {
	// Check cache first
	digestCacheMux.RLock()
	if entry, found := digestCache[image]; found && time.Now().Before(entry.expiresAt) {
		digestCacheMux.RUnlock()
		d.logger.Debug("Using cached digest for %s: %s", image, entry.digest)
		return entry.digest, nil
	}
	digestCacheMux.RUnlock()

	d.logger.Debug("Getting remote digest for %s using go-containerregistry", image)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse the image reference
	ref, err := name.ParseReference(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Get the image descriptor with timeout context
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		// Handle specific error types
		if strings.Contains(err.Error(), "unauthorized") {
			d.logger.Warn("Authentication error for %s: %v", image, err)
			return "", fmt.Errorf("authentication failed for image %s: %w", image, err)
		} else if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout while fetching image digest: %w", err)
		}
		return "", fmt.Errorf("failed to get image descriptor: %w", err)
	}

	// Get the digest
	digest := desc.Digest.String()
	d.logger.Debug("Remote digest for %s: %s", image, digest)

	// Cache the result
	digestCacheMux.Lock()
	digestCache[image] = digestCacheEntry{
		digest:    digest,
		expiresAt: time.Now().Add(digestCacheTTL),
	}
	digestCacheMux.Unlock()

	return digest, nil
}

// ShouldPullImage checks if the remote image is different from the local one
// Returns true if the image should be pulled, false otherwise, and any error encountered
func (d *Docker) ShouldPullImage(image string) (bool, error) {
	start := time.Now()
	defer func() {
		d.logger.Debug("ShouldPullImage check for %s took %v", image, time.Since(start))
	}()

	// Parse the image to ensure it's valid
	_, err := name.ParseReference(image)
	if err != nil {
		return true, fmt.Errorf("invalid image reference %s: %w", image, err)
	}

	// Try to get local digest
	localDigest, localErr := d.GetLocalImageDigest(image)
	
	// If local image doesn't exist, we definitely need to pull
	if localErr != nil {
		d.logger.Info("Local image %s not found, will pull", image)
		return true, nil
	}
	
	// Try to get remote digest
	remoteDigest, remoteErr := d.GetRemoteImageDigest(image)
	if remoteErr != nil {
		// Check for specific error types
		if strings.Contains(remoteErr.Error(), "not found") {
			d.logger.Error("Image %s not found in remote registry", image)
			return false, fmt.Errorf("image not found in registry: %w", remoteErr)
		} else if strings.Contains(remoteErr.Error(), "unauthorized") {
			d.logger.Warn("Authentication error for %s: %v", image, remoteErr)
			// For auth errors, we might want to retry with credentials, but for now just pull
			return true, fmt.Errorf("authentication error, will pull anyway: %w", remoteErr)
		} else {
			// For other errors, log warning but proceed with pull to be safe
			d.logger.Warn("Could not get remote digest for %s: %v, will pull anyway", image, remoteErr)
			return true, nil
		}
	}
	
	// Compare digests
	shouldPull := localDigest != remoteDigest
	if shouldPull {
		d.logger.Info("Remote image %s has different digest, will pull", image)
		d.logger.Debug("Local digest: %s", localDigest)
		d.logger.Debug("Remote digest: %s", remoteDigest)
	} else {
		d.logger.Info("Image %s is up to date, skipping pull", image)
		d.logger.Debug("Digest: %s", localDigest)
	}
	
	return shouldPull, nil
}
