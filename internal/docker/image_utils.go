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
// Uses go-containerregistry to ensure format consistency with remote digests
func (d *Docker) GetLocalImageDigest(image string) (string, error) {
	start := time.Now()
	defer func() {
		if time.Since(start) > 5*time.Second {
			d.logger.Warn("GetLocalImageDigest took longer than expected: %v", time.Since(start))
		}
	}()

	// First check if the image exists locally using Docker CLI
	// This is faster than using go-containerregistry for the existence check
	output, err := d.RunCommand("images", "--format", "{{.Repository}}:{{.Tag}}", image)
	if err != nil || strings.TrimSpace(output) == "" {
		d.logger.Debug("Image %s not found locally", image)
		return "", fmt.Errorf("image not found locally: %s", image)
	}

	// Parse the image reference
	ref, err := name.ParseReference(image)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the same library as GetRemoteImageDigest but with local image source
	// This ensures the digest format is consistent between local and remote
	img, err := remote.Image(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", fmt.Errorf("failed to get local image with go-containerregistry: %w", err)
	}

	// Get the digest
	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("failed to get digest for local image: %w", err)
	}

	digestStr := digest.String()
	d.logger.Debug("Local digest for %s: %s", image, digestStr)
	return digestStr, nil
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
	// First, normalize both digests to ensure they're in the same format
	localDigestNormalized := localDigest
	remoteDigestNormalized := remoteDigest
	
	// If either digest contains a full repo reference (e.g., "docker.io/library/image@sha256:123"),
	// extract just the digest part
	if strings.Contains(localDigestNormalized, "@") {
		parts := strings.Split(localDigestNormalized, "@")
		if len(parts) == 2 {
			localDigestNormalized = parts[1]
		}
	}
	
	if strings.Contains(remoteDigestNormalized, "@") {
		parts := strings.Split(remoteDigestNormalized, "@")
		if len(parts) == 2 {
			remoteDigestNormalized = parts[1]
		}
	}
	
	// Log both original and normalized digests for debugging
	d.logger.Debug("Local digest (original): %s", localDigest)
	d.logger.Debug("Local digest (normalized): %s", localDigestNormalized)
	d.logger.Debug("Remote digest (original): %s", remoteDigest)
	d.logger.Debug("Remote digest (normalized): %s", remoteDigestNormalized)
	
	// Compare normalized digests
	shouldPull := localDigestNormalized != remoteDigestNormalized
	if shouldPull {
		d.logger.Info("Remote image %s has different digest, will pull", image)
		d.logger.Info("Local digest: %s", localDigestNormalized)
		d.logger.Info("Remote digest: %s", remoteDigestNormalized)
	} else {
		d.logger.Info("Image %s is up to date, skipping pull", image)
		d.logger.Info("Digest: %s", localDigestNormalized)
	}
	
	return shouldPull, nil
}
