package utils

import (
	"fmt"
	"github.com/spf13/afero"
	"io"
)

func Touch(fs afero.Fs, dst string) error {
	file, err := fs.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	file.Close()
	return nil
}

func CopyFile(fs afero.Fs, src, dst string) error {
	sourceFile, err := fs.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := fs.Create(dst)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}

func MakeExecutable(fs afero.Fs, file string) error {
	info, err := fs.Stat(file)
	if err != nil {
		return err
	}

	newMode := info.Mode() | 0111

	if err := fs.Chmod(file, newMode); err != nil {
		return err
	}

	return nil
}
