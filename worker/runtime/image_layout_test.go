//go:build linux || windows

package runtime

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestReadImageLayout(t *testing.T) {
	for name, mediaType := range map[string]string{
		"OCI manifest":    ocispec.MediaTypeImageManifest,
		"Docker manifest": images.MediaTypeDockerSchema2Manifest,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			base, baseDiffID := writeLayer(t, dir, "base")
			update, updateDiffID := writeLayer(t, dir, "update")
			diffIDs := []digest.Digest{baseDiffID, updateDiffID}
			writeIndex(t, dir, writeImage(t, dir, mediaType, []ocispec.Descriptor{base, update}, diffIDs))

			layers, err := readImageLayout(dir)
			require.NoError(t, err)
			require.Equal(t, []imageLayer{
				{Blob: filepath.Join(dir, "blobs", "sha256", base.Digest.Encoded()), ChainID: diffIDs[0]},
				{Blob: filepath.Join(dir, "blobs", "sha256", update.Digest.Encoded()), ChainID: identity.ChainID(diffIDs)},
			}, layers)
		})
	}
}

func TestReadImageLayoutWithoutIndex(t *testing.T) {
	_, err := readImageLayout(t.TempDir())
	require.ErrorContains(t, err, "not an OCI image layout")
	require.ErrorContains(t, err, "registry-image")
}

func TestReadImageLayoutWithTwoManifests(t *testing.T) {
	dir := t.TempDir()
	ltsc2019, ltsc2019DiffID := writeLayer(t, dir, "ltsc2019")
	ltsc2022, ltsc2022DiffID := writeLayer(t, dir, "ltsc2022")
	writeIndex(t, dir,
		writeImage(t, dir, ocispec.MediaTypeImageManifest, []ocispec.Descriptor{ltsc2019}, []digest.Digest{ltsc2019DiffID}),
		writeImage(t, dir, ocispec.MediaTypeImageManifest, []ocispec.Descriptor{ltsc2022}, []digest.Digest{ltsc2022DiffID}),
	)

	_, err := readImageLayout(dir)
	require.Error(t, err)
}

func TestReadImageLayoutWithoutLayers(t *testing.T) {
	dir := t.TempDir()
	writeIndex(t, dir, writeImage(t, dir, ocispec.MediaTypeImageManifest, nil, nil))

	_, err := readImageLayout(dir)
	require.Error(t, err)
}

func TestReadImageLayoutWithMissingDiffID(t *testing.T) {
	dir := t.TempDir()
	base, baseDiffID := writeLayer(t, dir, "base")
	update, _ := writeLayer(t, dir, "update")
	writeIndex(t, dir, writeImage(t, dir, ocispec.MediaTypeImageManifest, []ocispec.Descriptor{base, update}, []digest.Digest{baseDiffID}))

	_, err := readImageLayout(dir)
	require.Error(t, err)
}

func TestReadImageLayoutWithMalformedDigest(t *testing.T) {
	const outside = digest.Digest("sha256:../../outside")

	t.Run("layer digest", func(t *testing.T) {
		dir := t.TempDir()
		layer, diffID := writeLayer(t, dir, "layer")
		layer.Digest = outside
		writeIndex(t, dir, writeImage(t, dir, ocispec.MediaTypeImageManifest, []ocispec.Descriptor{layer}, []digest.Digest{diffID}))

		_, err := readImageLayout(dir)
		require.Error(t, err)
	})

	t.Run("diff ID", func(t *testing.T) {
		dir := t.TempDir()
		layer, _ := writeLayer(t, dir, "layer")
		writeIndex(t, dir, writeImage(t, dir, ocispec.MediaTypeImageManifest, []ocispec.Descriptor{layer}, []digest.Digest{outside}))

		_, err := readImageLayout(dir)
		require.Error(t, err)
	})
}

func writeLayer(t *testing.T, dir, content string) (ocispec.Descriptor, digest.Digest) {
	t.Helper()

	var blob bytes.Buffer
	zw := gzip.NewWriter(&blob)
	_, err := zw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	return writeBlob(t, dir, ocispec.MediaTypeImageLayerGzip, blob.Bytes()), digest.FromString(content)
}

func writeImage(t *testing.T, dir, mediaType string, layers []ocispec.Descriptor, diffIDs []digest.Digest) ocispec.Descriptor {
	t.Helper()

	config := writeBlob(t, dir, ocispec.MediaTypeImageConfig, marshal(t, ocispec.Image{
		Platform: ocispec.Platform{OS: "windows", Architecture: "amd64"},
		RootFS:   ocispec.RootFS{Type: "layers", DiffIDs: diffIDs},
	}))

	return writeBlob(t, dir, mediaType, marshal(t, ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: mediaType,
		Config:    config,
		Layers:    layers,
	}))
}

func writeIndex(t *testing.T, dir string, manifests ...ocispec.Descriptor) {
	t.Helper()

	index := marshal(t, ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), index, 0o644))
}

func writeBlob(t *testing.T, dir, mediaType string, data []byte) ocispec.Descriptor {
	t.Helper()

	d := digest.FromBytes(data)
	path := filepath.Join(dir, "blobs", "sha256", d.Encoded())
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))

	return ocispec.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return data
}
