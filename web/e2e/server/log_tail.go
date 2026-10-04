package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// applicationLogPath keeps the application's output from the latest browser run for post-mortem reading.
const applicationLogPath = ".test-docker/e2e/application.log"

// applicationLogTailBytes bounds how much of the application log an early exit prints, enough for a startup failure without flooding the Playwright output.
const applicationLogTailBytes = 64 << 10

// copyLogTail copies the last maxBytes of the log at path to writer.
func copyLogTail(writer io.Writer, path string, maxBytes int64) error {
	if maxBytes < 1 {
		return fmt.Errorf("log tail bound must be positive, got %d", maxBytes)
	}
	logFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	info, err := logFile.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("application log is not a regular file")
	}
	if _, err := logFile.Seek(max(info.Size()-maxBytes, 0), io.SeekStart); err != nil {
		return err
	}
	_, err = io.Copy(writer, logFile)
	return err
}
