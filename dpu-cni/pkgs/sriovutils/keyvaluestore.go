package sriovutils

import (
	"fmt"
	"github.com/spf13/afero"
	"os"
	"path/filepath"
)

type KeyValueStore struct {
	dataDir string
	fs      afero.Fs
}

func NewKeyValueStore(dataDir string) *KeyValueStore {
	return &KeyValueStore{
		dataDir: filepath.Join(dataDir, "store"),
		fs:      afero.NewOsFs(),
	}
}

func (K *KeyValueStore) Set(key, value string) error {
	if err := K.fs.MkdirAll(K.dataDir, 0600); err != nil {
		return fmt.Errorf("failed to create the data directory %q : %v", K.dataDir, err)
	}
	path := K.path(key)
	err := afero.WriteFile(K.fs, path, []byte(value), 0600)
	if err != nil {
		return fmt.Errorf("failed to write value for key %q in the path %q : %v", key, path, err)
	}
	return err
}

func (K *KeyValueStore) Delete(key string) error {
	return K.fs.Remove(K.path(key))
}

func (K *KeyValueStore) Get(key string) (string, error) {
	path := K.path(key)
	dat, err := afero.ReadFile(K.fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		return "", fmt.Errorf("failed to read for key file for %s: %v", path, err)
	}
	return string(dat), nil
}

func (K *KeyValueStore) path(key string) string {
	return filepath.Join(K.dataDir, key)
}
