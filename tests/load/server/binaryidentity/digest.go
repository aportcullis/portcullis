// Package binaryidentity identifies the application artifact used by a load run.
package binaryidentity

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// DigestFile reports the SHA-256 identity without retaining the application bytes.
func DigestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}
