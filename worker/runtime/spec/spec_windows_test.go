//go:build windows

package spec_test

import (
	"testing"

	"code.cloudfoundry.org/garden"
	"github.com/concourse/concourse/worker/runtime/spec"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func TestOciSpec(t *testing.T) {
	gdn := garden.ContainerSpec{
		Handle:     "handle",
		RootFSPath: `raw://C:\rootfs`,
		Env:        []string{"foo=bar"},
		Limits: garden.Limits{
			Memory: garden.MemoryLimits{LimitInBytes: 1024},
		},
	}

	oci, err := spec.OciSpec(spec.DefaultInitBinPath, specs.LinuxSeccomp{}, specs.LinuxSeccomp{}, specs.Hooks{}, spec.FullPrivilegedMode, gdn, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	if oci.Root.Path != `C:\rootfs` {
		t.Errorf("unexpected root path: %s", oci.Root.Path)
	}

	if oci.Windows == nil {
		t.Fatal("expected a Windows section")
	}

	if *oci.Windows.Resources.Memory.Limit != 1024 {
		t.Errorf("unexpected memory limit: %d", *oci.Windows.Resources.Memory.Limit)
	}

	if oci.Process.Args[0] != `C:\concourse\bin\init.exe` {
		t.Errorf("unexpected process args: %v", oci.Process.Args)
	}

	foundInitMount := false
	for _, m := range oci.Mounts {
		if m.Destination == spec.InitBinContainerDir {
			foundInitMount = true
		}
	}
	if !foundInitMount {
		t.Errorf("expected a mount for the init binary dir, got: %v", oci.Mounts)
	}
}

func TestOciSpecEscapedRootfs(t *testing.T) {
	gdn := garden.ContainerSpec{
		Handle:     "handle",
		RootFSPath: `raw://C:%5Cworkdir%5Cvolumes%5Clive%5Cguid%5Cvolume/rootfs`,
	}

	oci, err := spec.OciSpec(spec.DefaultInitBinPath, specs.LinuxSeccomp{}, specs.LinuxSeccomp{}, specs.Hooks{}, spec.FullPrivilegedMode, gdn, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	expected := `C:\workdir\volumes\live\guid\volume\rootfs`
	if oci.Root.Path != expected {
		t.Errorf("unexpected root path: %s", oci.Root.Path)
	}
}

func TestOciSpecRequiresHandle(t *testing.T) {
	_, err := spec.OciSpec(spec.DefaultInitBinPath, specs.LinuxSeccomp{}, specs.LinuxSeccomp{}, specs.Hooks{}, spec.FullPrivilegedMode, garden.ContainerSpec{}, 0, 0)
	if err == nil {
		t.Fatal("expected an error for a spec without a handle")
	}
}
