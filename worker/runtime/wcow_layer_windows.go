//go:build windows

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/hcsshim"
	"github.com/Microsoft/hcsshim/pkg/ociwclayer"
	"github.com/containerd/containerd/v2/pkg/archive/compression"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// layerChainFile is written last and marks the layer's import as complete.
const layerChainFile = "layerchain.json"

var layerImportLock sync.Mutex

// prepareContainer converts the container's rootfs into Windows container
// layers and attaches the container to the worker's network.
func (b *GardenBackend) prepareContainer(oci *specs.Spec, handle string) error {
	err := b.prepareRootfs(oci)
	if err != nil {
		return fmt.Errorf("prepare rootfs: %w", err)
	}

	if attacher, ok := b.network.(ContainerAttacher); ok {
		err = attacher.AttachContainer(handle, oci)
		if err != nil {
			return fmt.Errorf("attach container network: %w", err)
		}
	}

	return nil
}

// prepareRootfs imports the layers of the OCI image layout in the container's
// rootfs directory and creates the scratch layer that the runhcs shim
// requires, mirroring what containerd's Windows snapshotter does when
// unpacking an image. The runtime mounts the layers itself, so the spec's
// Root is cleared in favour of Windows.LayerFolders.
func (b *GardenBackend) prepareRootfs(oci *specs.Spec) error {
	if oci.Root == nil || oci.Root.Path == "" {
		return fmt.Errorf("rootfs not set in spec")
	}

	layers, err := readImageLayout(oci.Root.Path)
	if err != nil {
		return fmt.Errorf("read image layout: %w", err)
	}

	folders, err := b.importLayers(layers)
	if err != nil {
		return err
	}

	scratchDir := filepath.Join(filepath.Dir(oci.Root.Path), "scratch")
	sandbox := filepath.Join(scratchDir, "sandbox.vhdx")
	if _, err := os.Stat(sandbox); os.IsNotExist(err) {
		err = os.MkdirAll(scratchDir, 0755)
		if err != nil {
			return fmt.Errorf("mkdir scratch dir: %w", err)
		}

		err = copyFile(filepath.Join(folders[len(folders)-1], "blank.vhdx"), sandbox)
		if err != nil {
			return fmt.Errorf("create sandbox vhdx: %w", err)
		}
	}

	oci.Root = nil
	oci.Windows.LayerFolders = append(folders, scratchDir)

	return nil
}

func (b *GardenBackend) importLayers(layers []imageLayer) ([]string, error) {
	layerImportLock.Lock()
	defer layerImportLock.Unlock()

	err := winio.EnableProcessPrivileges([]string{winio.SeBackupPrivilege, winio.SeRestorePrivilege})
	if err != nil {
		return nil, fmt.Errorf("enable backup and restore privileges: %w", err)
	}

	// parents go nearest first, as containerd's Windows snapshotter passes them
	parents := []string{}
	for _, layer := range layers {
		dir, err := b.importLayer(layer, parents)
		if err != nil {
			return nil, fmt.Errorf("import layer %s: %w", layer.ChainID, err)
		}

		parents = append([]string{dir}, parents...)
	}

	return parents, nil
}

func (b *GardenBackend) importLayer(layer imageLayer, parents []string) (string, error) {
	id := layer.ChainID.Encoded()
	dir := filepath.Join(b.layersDir, id)
	chainFile := filepath.Join(dir, layerChainFile)

	_, err := os.Stat(chainFile)
	if err == nil {
		return dir, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	if _, err := os.Stat(dir); err == nil {
		err = hcsshim.DestroyLayer(hcsshim.DriverInfo{HomeDir: b.layersDir}, id)
		if err != nil {
			return "", fmt.Errorf("destroy incomplete import: %w", err)
		}
	}

	blob, err := os.Open(layer.Blob)
	if err != nil {
		return "", err
	}

	tar, err := compression.DecompressStream(blob)
	if err != nil {
		return "", errors.Join(fmt.Errorf("decompress: %w", err), blob.Close())
	}

	err = winio.RunWithPrivileges([]string{winio.SeSecurityPrivilege}, func() error {
		_, err := ociwclayer.ImportLayerFromTar(context.Background(), tar, dir, parents)
		return err
	})
	err = errors.Join(err, tar.Close(), blob.Close())
	if err != nil {
		return "", err
	}

	chain, err := json.Marshal(parents)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(chainFile, chain, 0644)
	if err != nil {
		return "", err
	}

	return dir, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}

	return closeErr
}
