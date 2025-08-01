package images

import "fmt"

type DummyImageManager struct{}

func NewDummyImageManager() *DummyImageManager {
	return &DummyImageManager{}
}

func (m *DummyImageManager) GetImage(key string) (string, error) {
	// Validate that key is one of the known image keys
	validKeys := m.GetAllKeys()
	isValid := false
	for _, validKey := range validKeys {
		if key == validKey {
			isValid = true
			break
		}
	}
	if !isValid {
		return "", fmt.Errorf("invalid image key %s: %w", key, ErrImageNotFound)
	}

	return fmt.Sprintf("%s-mock-image", key), nil
}

func (m *DummyImageManager) GetAllKeys() []string {
	return AllImageKeys()
}
