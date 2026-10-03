package binaryidentity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aportcullis/portcullis/tests/load/server/binaryidentity"
)

func TestManifestIdentityMatchesApplicationAndPreservesArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := binaryidentity.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("manifest identifies different application bytes: %s", digest)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "abc" {
		t.Fatal("identity inspection changed the application artifact")
	}
}

func TestMissingApplicationCannotProduceManifestIdentity(t *testing.T) {
	digest, err := binaryidentity.DigestFile(filepath.Join(t.TempDir(), "missing"))
	if !os.IsNotExist(err) || digest != "" {
		t.Fatal("missing application produced a usable identity")
	}
}
