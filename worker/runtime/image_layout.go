//go:build linux || windows

package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type imageLayer struct {
	Blob    string
	ChainID digest.Digest
}

func readImageLayout(dir string) ([]imageLayer, error) {
	var index ocispec.Index
	err := readJSON(filepath.Join(dir, ocispec.ImageIndexFile), &index)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("rootfs is not an OCI image layout, fetch Windows images with registry-image: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ocispec.ImageIndexFile, err)
	}

	if len(index.Manifests) != 1 {
		return nil, fmt.Errorf("%s lists %d manifests, expected one image", ocispec.ImageIndexFile, len(index.Manifests))
	}

	desc := index.Manifests[0]
	if desc.MediaType != ocispec.MediaTypeImageManifest && desc.MediaType != images.MediaTypeDockerSchema2Manifest {
		return nil, fmt.Errorf("%s lists a %s, expected an image manifest", ocispec.ImageIndexFile, desc.MediaType)
	}

	var manifest ocispec.Manifest
	err = readBlob(dir, desc.Digest, &manifest)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var config ocispec.Image
	err = readBlob(dir, manifest.Config.Digest, &config)
	if err != nil {
		return nil, fmt.Errorf("read image config: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("image has no layers")
	}

	diffIDs := config.RootFS.DiffIDs
	if len(diffIDs) != len(manifest.Layers) {
		return nil, fmt.Errorf("image config has %d diff IDs for %d layers", len(diffIDs), len(manifest.Layers))
	}

	for _, diffID := range diffIDs {
		err = diffID.Validate()
		if err != nil {
			return nil, fmt.Errorf("diff ID %q: %w", diffID, err)
		}
	}

	chainIDs := identity.ChainIDs(diffIDs)

	layers := make([]imageLayer, len(manifest.Layers))
	for i, layer := range manifest.Layers {
		blob, err := blobPath(dir, layer.Digest)
		if err != nil {
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}

		layers[i] = imageLayer{Blob: blob, ChainID: chainIDs[i]}
	}

	return layers, nil
}

func blobPath(dir string, d digest.Digest) (string, error) {
	err := d.Validate()
	if err != nil {
		return "", fmt.Errorf("digest %q: %w", d, err)
	}

	return filepath.Join(dir, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Encoded()), nil
}

func readBlob(dir string, d digest.Digest, v any) error {
	path, err := blobPath(dir, d)
	if err != nil {
		return err
	}

	return readJSON(path, v)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, v)
}
