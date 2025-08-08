package utils

import (
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// FilesystemMode represents the deployment mode of the operating system
type FilesystemMode string

const (
	// ImageMode represents immutable, image-based deployment (OSTree, etc.)
	ImageMode FilesystemMode = "ImageMode"
	// PackageMode represents traditional, package-based deployment (rpm, deb, etc.)
	PackageMode FilesystemMode = "PackageMode"
)

// FilesystemModeDetector detects whether the operating system uses
// image-based deployment (immutable) or package-based deployment (mutable)
type FilesystemModeDetector struct {
	fs afero.Fs
}

// NewFilesystemModeDetector creates a new FilesystemModeDetector with the OS filesystem
func NewFilesystemModeDetector() *FilesystemModeDetector {
	return &FilesystemModeDetector{
		fs: afero.NewOsFs(),
	}
}

// NewFilesystemModeDetectorWithFs creates a FilesystemModeDetector with a custom filesystem interface.
// This is primarily used for testing with mock filesystems.
func NewFilesystemModeDetectorWithFs(fs afero.Fs) *FilesystemModeDetector {
	return &FilesystemModeDetector{
		fs: fs,
	}
}

// DetectMode returns the current filesystem deployment mode
func (fmd *FilesystemModeDetector) DetectMode() (FilesystemMode, error) {
	isImageMode, err := fmd.IsImageMode()
	if err != nil {
		return "", err
	}

	if isImageMode {
		return ImageMode, nil
	}
	return PackageMode, nil
}

// IsImageMode checks if the OS uses immutable image-based deployment
// Currently detects OSTree-based systems, but could be extended for other image-based systems
func (fmd *FilesystemModeDetector) IsImageMode() (bool, error) {
	// When running in the controller container, check the mounted host path
	// Otherwise, check the local path (for daemon containers)
	paths := []string{"/host-run/ostree-booted", "/run/ostree-booted"}

	for _, path := range paths {
		if _, err := fmd.fs.Stat(path); err == nil {
			return true, nil // Image-based OS detected (file accessible)
		} else if os.IsPermission(err) {
			return true, nil // Image-based OS detected (file exists but permission denied)
		} else if os.IsNotExist(err) {
			// File doesn't exist, continue to next path
			continue
		} else {
			return false, fmt.Errorf("Failed to check image-based OS status at %s: %v", path, err)
		}
	}

	return false, nil // Not an image-based OS (file doesn't exist on any path)
}

// IsPackageMode checks if the OS uses traditional package-based deployment (rpm, deb, etc.)
// Package mode is the inverse of image mode
func (fmd *FilesystemModeDetector) IsPackageMode() (bool, error) {
	isImageMode, err := fmd.IsImageMode()
	if err != nil {
		return false, err
	}
	// Package mode is the inverse of image mode
	return !isImageMode, nil
}
