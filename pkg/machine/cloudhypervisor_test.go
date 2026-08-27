package machine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spectrocloud/peg/pkg/machine/types"
)

func cfg() types.MachineConfig {
	return types.MachineConfig{
		StateDir: "/state",
		CPU:      "2",
		Memory:   "2048",
		SSH:      &types.SSH{Port: "2222"},
	}
}

func joined(args []string) string { return strings.Join(args, " ") }

func TestBuildArgsFirmwareDrives(t *testing.T) {
	args := buildCloudHypervisorArgs(cfg(), "/fw/hypervisor-fw", []string{"/state/d0.img"})
	got := joined(args)
	for _, want := range []string{
		"--api-socket /state/ch-api.sock",
		"--cpus boot=2",
		"--memory size=2048M",
		"--firmware /fw/hypervisor-fw",
		"--disk path=/state/d0.img",
		"--net tap=,mac=",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in: %s", want, got)
		}
	}
}

func TestBuildArgsDatasourceReadonly(t *testing.T) {
	c := cfg()
	c.DataSource = "/state/seed.img"
	args := buildCloudHypervisorArgs(c, "/fw", []string{"/state/d0.img"})
	if !strings.Contains(joined(args), "path=/state/seed.img,readonly=on") {
		t.Fatalf("datasource not attached readonly: %s", joined(args))
	}
}

func TestBuildArgsNetworkingDisabled(t *testing.T) {
	c := cfg()
	c.DisableDefaultNetworking = true
	args := buildCloudHypervisorArgs(c, "/fw", []string{"/state/d0.img"})
	if strings.Contains(joined(args), "--net") {
		t.Fatalf("--net present despite disabled networking: %s", joined(args))
	}
}

func TestBuildArgsAppendsUserArgs(t *testing.T) {
	c := cfg()
	c.Args = []string{"--rng", "src=/dev/urandom"}
	args := buildCloudHypervisorArgs(c, "/fw", nil)
	if !strings.HasSuffix(joined(args), "--rng src=/dev/urandom") {
		t.Fatalf("user args not appended last: %s", joined(args))
	}
}

func TestBuildLaunchCommandPasta(t *testing.T) {
	name, args := buildLaunchCommand(cfg(), "/usr/bin/pasta", "/bin/cloud-hypervisor", []string{"--cpus", "boot=2"})
	if name != "/usr/bin/pasta" {
		t.Fatalf("expected /usr/bin/pasta, got %s", name)
	}
	got := joined(args)
	for _, want := range []string{"--config-net", "-t 2222:22", "-- /bin/cloud-hypervisor", "--cpus boot=2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in: %s", want, got)
		}
	}
}

func TestBuildLaunchCommandNoNet(t *testing.T) {
	c := cfg()
	c.DisableDefaultNetworking = true
	name, args := buildLaunchCommand(c, "", "/bin/cloud-hypervisor", []string{"--cpus", "boot=2"})
	if name != "/bin/cloud-hypervisor" {
		t.Fatalf("expected direct binary, got %s", name)
	}
	if joined(args) != "--cpus boot=2" {
		t.Fatalf("args altered: %s", joined(args))
	}
}

func TestBuildLaunchCommandPastaUsesResolvedPath(t *testing.T) {
	resolvedPasta := "/usr/local/bin/pasta"
	name, _ := buildLaunchCommand(cfg(), resolvedPasta, "/bin/cloud-hypervisor", []string{"--cpus", "boot=2"})
	if name != resolvedPasta {
		t.Fatalf("expected process name to be resolved absolute path %q, got %q", resolvedPasta, name)
	}
	if name == "pasta" {
		t.Fatal("process name must not be bare 'pasta': os.StartProcess does no PATH lookup")
	}
}

func TestFirstExisting(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "fw")
	if err := os.WriteFile(real, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := firstExisting([]string{filepath.Join(dir, "missing"), real})
	if err != nil || got != real {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := firstExisting([]string{filepath.Join(dir, "none")}); err == nil {
		t.Fatal("expected error when no path exists")
	}
}

func TestNewSelectsCloudHypervisor(t *testing.T) {
	m, err := New(types.WithStateDir(t.TempDir()), types.CloudHypervisorEngine,
		types.WithSSHPort("2222"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := m.(*CloudHypervisor); !ok {
		t.Fatalf("expected *CloudHypervisor, got %T", m)
	}
}

func TestCloudHypervisorImplementsMachine(t *testing.T) {
	var _ types.Machine = (*CloudHypervisor)(nil)
}

func TestScreenshotNotImplemented(t *testing.T) {
	c := &CloudHypervisor{machineConfig: cfg()}
	if _, err := c.Screenshot(); err == nil {
		t.Fatal("expected screenshot error")
	}
}

func TestDetachCDNoop(t *testing.T) {
	c := &CloudHypervisor{machineConfig: cfg()}
	if err := c.DetachCD(); err != nil {
		t.Fatalf("DetachCD should be nil, got %v", err)
	}
}
