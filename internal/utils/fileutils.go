package utils

import (
	"io"
	"os"
)

func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
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

func MakeExecutable(file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}

	newMode := info.Mode() | 0111

	if err := os.Chmod(file, newMode); err != nil {
		return err
	}

	return nil
}
