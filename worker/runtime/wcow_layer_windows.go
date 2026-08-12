//go:build windows

package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Microsoft/hcsshim"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// prepareRootfs converts the container's rootfs directory into a Windows
// container base layer and creates the scratch layer that the runhcs shim
// requires, mirroring what containerd's Windows snapshotter does when
// unpacking an image. The runtime mounts the layers itself, so the spec's
// Root is cleared in favour of Windows.LayerFolders.
func (b *GardenBackend) prepareRootfs(oci *specs.Spec) error {
	if oci.Root == nil || oci.Root.Path == "" {
		return fmt.Errorf("rootfs not set in spec")
	}

	rootfs := oci.Root.Path
	volumeDir := filepath.Dir(rootfs)

	// hcsshim expects the layer's content under Files/. Images extracted
	// from OCI Windows layers already have that structure; plain directory
	// trees get moved underneath it.
	layerDir := rootfs
	if _, err := os.Stat(filepath.Join(rootfs, "Files")); os.IsNotExist(err) {
		layerDir = filepath.Join(volumeDir, "layer")
		if _, err := os.Stat(layerDir); os.IsNotExist(err) {
			err = os.Mkdir(layerDir, 0755)
			if err != nil {
				return fmt.Errorf("mkdir layer dir: %w", err)
			}

			err = os.Rename(rootfs, filepath.Join(layerDir, "Files"))
			if err != nil {
				return fmt.Errorf("move rootfs into layer: %w", err)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(layerDir, "blank.vhdx")); os.IsNotExist(err) {
		// Flattened multi-layer images carry per-layer artifacts that the
		// base layer processing wants to create itself (Hives) or that only
		// hyper-v isolation would use (UtilityVM); processing fails if
		// they're present.
		for _, artifact := range []string{"UtilityVM", "Hives"} {
			err = os.RemoveAll(filepath.Join(layerDir, artifact))
			if err != nil {
				return fmt.Errorf("remove layer artifact %s: %w", artifact, err)
			}
		}

		// ConvertToBaseLayer creates minimal registry hives but fails on
		// images that already ship them; process those directly.
		systemHive := filepath.Join(layerDir, "Files", "Windows", "System32", "config", "SYSTEM")
		if _, err := os.Stat(systemHive); err == nil {
			err = hcsshim.ProcessBaseLayer(layerDir)
			if err != nil {
				return fmt.Errorf("process base layer: %w", err)
			}
		} else {
			err = hcsshim.ConvertToBaseLayer(layerDir)
			if err != nil {
				return fmt.Errorf("convert to base layer: %w", err)
			}
		}
	}

	scratchDir := filepath.Join(volumeDir, "scratch")
	sandbox := filepath.Join(scratchDir, "sandbox.vhdx")
	if _, err := os.Stat(sandbox); os.IsNotExist(err) {
		err = os.MkdirAll(scratchDir, 0755)
		if err != nil {
			return fmt.Errorf("mkdir scratch dir: %w", err)
		}

		err = copyFile(filepath.Join(layerDir, "blank.vhdx"), sandbox)
		if err != nil {
			return fmt.Errorf("create sandbox vhdx: %w", err)
		}
	}

	oci.Root = nil
	oci.Windows.LayerFolders = []string{layerDir, scratchDir}

	return nil
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
