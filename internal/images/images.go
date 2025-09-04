package images

import "fmt"

// Image environment variable names
const (
	VspImageIntel          = "IntelVspImage"
	VspImageMarvell        = "MarvellVspImage"
	VspImageIntelNetSec    = "IntelNetSecVspImage"
	VspImageP4Intel        = "IntelVspP4Image"
	VspImageMarvellCpAgent = "MarvellVspCpAgentImage"
	DpuOperatorDaemonImage = "DpuOperatorDaemonImage"
	NRIWebhookImage        = "NRIWebhookImage"
)

type ImageManager interface {
	GetImage(key string) (string, error)
	GetAllKeys() []string
}

var ErrImageNotFound = fmt.Errorf("image not found")

// AllImageKeys returns all known image keys
func AllImageKeys() []string {
	return []string{
		DpuOperatorDaemonImage,
		NRIWebhookImage,
		VspImageIntel,
		VspImageMarvell,
		VspImageIntelNetSec,
		VspImageP4Intel,
		VspImageMarvellCpAgent,
	}
}

// NewDummyManager creates a test image manager that returns mock images
func NewDummyManager() ImageManager {
	return NewDummyImageManager()
}

// MergeVarsWithImages gets all images from ImageManager and merges with additional vars
func MergeVarsWithImages(imageManager ImageManager, additionalVars map[string]string) map[string]string {
	vars := make(map[string]string)

	// Start with all images
	for _, key := range imageManager.GetAllKeys() {
		if value, err := imageManager.GetImage(key); err == nil {
			vars[key] = value
		} else {
			vars[key] = ""
		}
	}

	// Add additional vars (these can override images if needed)
	for k, v := range additionalVars {
		vars[k] = v
	}

	return vars
}
