package images

import (
	"fmt"
	"os"
)

type EnvImageManager struct{}

func NewEnvImageManager() *EnvImageManager {
	return &EnvImageManager{}
}

func (m *EnvImageManager) GetImage(key string) (string, error) {
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

	val := os.Getenv(key)
	if val == "" {
		return "", fmt.Errorf("environment variable %s not set: %w", key, ErrImageNotFound)
	}
	return val, nil
}

func (m *EnvImageManager) GetAllKeys() []string {
	return AllImageKeys()
}
