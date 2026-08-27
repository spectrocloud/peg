package machine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	process "github.com/mudler/go-processmanager"
	"github.com/spectrocloud/peg/internal/utils"
	controller "github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

type CloudHypervisor struct {
	machineConfig types.MachineConfig
	process       *process.Process
}

// commonCHBinaryPaths are searched (in order) when no Process override is set.
var commonCHBinaryPaths = []string{
	"/usr/bin/cloud-hypervisor",
	"/usr/local/bin/cloud-hypervisor",
	"/home/linuxbrew/.linuxbrew/bin/cloud-hypervisor",
}

// commonPastaPaths are searched (in order) when resolving the pasta binary.
var commonPastaPaths = []string{
	"/usr/bin/pasta",
	"/usr/local/bin/pasta",
}

// commonFirmwarePaths are searched (in order) when no Firmware is configured.
var commonFirmwarePaths = []string{
	"/usr/share/cloud-hypervisor/hypervisor-fw",
	"/usr/lib/cloud-hypervisor/hypervisor-fw",
	"/usr/share/cloud-hypervisor/CLOUDHV.fd",
	"/usr/share/edk2/x64/CLOUDHV.fd",
}

func firstExisting(paths []string) (string, error) {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("none of the candidate paths exist: %v", paths)
}

// buildCloudHypervisorArgs builds the cloud-hypervisor CLI args (excluding the
// binary name and any pasta wrapper). Pure function for testability.
func buildCloudHypervisorArgs(mc types.MachineConfig, firmware string, drives []string) []string {
	args := []string{
		"--api-socket", filepath.Join(mc.StateDir, "ch-api.sock"),
		"--cpus", fmt.Sprintf("boot=%s", mc.CPU),
		"--memory", fmt.Sprintf("size=%sM", mc.Memory),
		"--firmware", firmware,
		"--serial", "tty",
		"--console", "off",
	}

	disks := []string{}
	for _, d := range drives {
		disks = append(disks, fmt.Sprintf("path=%s", d))
	}
	if mc.DataSource != "" {
		disks = append(disks, fmt.Sprintf("path=%s,readonly=on", mc.DataSource))
	}
	if len(disks) > 0 {
		args = append(args, "--disk")
		args = append(args, disks...)
	}

	if !mc.DisableDefaultNetworking {
		args = append(args, "--net", "tap=,mac=")
	}

	args = append(args, mc.Args...)
	return args
}

// buildLaunchCommand wraps the cloud-hypervisor invocation with pasta when
// default networking is enabled. Pasta builds a rootless netns + tap and
// forwards host <SSH.Port> -> guest 22, preserving 127.0.0.1:<port> SSH.
// pastaBinary must be an absolute path (resolved by resolvePasta before calling).
// When DisableDefaultNetworking is true, pastaBinary is ignored.
func buildLaunchCommand(mc types.MachineConfig, pastaBinary string, chBinary string, chArgs []string) (string, []string) {
	if mc.DisableDefaultNetworking {
		return chBinary, chArgs
	}
	port := ""
	if mc.SSH != nil {
		port = mc.SSH.Port
	}
	pastaArgs := []string{"--config-net", "-t", fmt.Sprintf("%s:22", port), "--", chBinary}
	return pastaBinary, append(pastaArgs, chArgs...)
}

func (q *CloudHypervisor) resolveBinary() (string, error) {
	if q.machineConfig.Process != "" {
		return q.machineConfig.Process, nil
	}
	if p, err := firstExisting(commonCHBinaryPaths); err == nil {
		return p, nil
	}
	p, err := exec.LookPath("cloud-hypervisor")
	if err != nil {
		return "", fmt.Errorf("cloud-hypervisor not found in common paths or PATH: %w", err)
	}
	return p, nil
}

func (q *CloudHypervisor) resolveFirmware() (string, error) {
	if q.machineConfig.Firmware != "" {
		return q.machineConfig.Firmware, nil
	}
	fw, err := firstExisting(commonFirmwarePaths)
	if err != nil {
		return "", fmt.Errorf("no firmware configured and none found in common paths: %w", err)
	}
	return fw, nil
}

func resolvePasta() (string, error) {
	if p, err := firstExisting(commonPastaPaths); err == nil {
		return p, nil
	}
	p, err := exec.LookPath("pasta")
	if err != nil {
		return "", fmt.Errorf("pasta not found in common paths or PATH: %w", err)
	}
	return p, nil
}

func (q *CloudHypervisor) driveSizes() []string {
	sizes := []string{}
	for _, s := range q.machineConfig.DriveSizes {
		sizes = append(sizes, fmt.Sprintf("%sM", s))
	}
	if len(sizes) == 0 {
		sizes = append(sizes, fmt.Sprintf("%sM", types.DefaultDriveSize))
	}
	return sizes
}

func (q *CloudHypervisor) CreateDisk(diskname, size string) error {
	if err := os.MkdirAll(q.machineConfig.StateDir, os.ModePerm); err != nil {
		return err
	}
	out, err := utils.SH(fmt.Sprintf("qemu-img create -f qcow2 %s %s",
		filepath.Join(q.machineConfig.StateDir, diskname), size))
	if err != nil {
		return fmt.Errorf("%s : %w", out, err)
	}
	return nil
}

func (q *CloudHypervisor) Create(ctx context.Context) (context.Context, error) {
	log.Info("Create cloud-hypervisor machine")

	if q.machineConfig.ISO != "" {
		log.Warn("cloud-hypervisor cannot boot ISO images; 'iso' is ignored. Provide a bootable disk image via 'image'/'drives'.")
	}

	userDrives := q.machineConfig.Drives
	if q.machineConfig.AutoDriveSetup && len(userDrives) == 0 {
		for i, s := range q.driveSizes() {
			filename := fmt.Sprintf("%s-%d.img", q.machineConfig.ID, i)
			if err := q.CreateDisk(filename, s); err != nil {
				return ctx, fmt.Errorf("creating disk with size %s: %w", s, err)
			}
			userDrives = append(userDrives, filepath.Join(q.machineConfig.StateDir, filename))
		}
	}

	binary, err := q.resolveBinary()
	if err != nil {
		return ctx, fmt.Errorf("failed to find cloud-hypervisor binary: %w", err)
	}
	firmware, err := q.resolveFirmware()
	if err != nil {
		return ctx, fmt.Errorf("failed to resolve firmware: %w", err)
	}

	chArgs := buildCloudHypervisorArgs(q.machineConfig, firmware, userDrives)

	pastaBinary := ""
	if !q.machineConfig.DisableDefaultNetworking {
		pastaBinary, err = resolvePasta()
		if err != nil {
			return ctx, fmt.Errorf("failed to find pasta: %w", err)
		}
	}
	name, args := buildLaunchCommand(q.machineConfig, pastaBinary, binary, chArgs)

	log.Infof("Starting VM with %s [ Memory: %s, CPU: %s ]", binary, q.machineConfig.Memory, q.machineConfig.CPU)

	p := process.New(
		process.WithName(name),
		process.WithArgs(args...),
		process.WithStateDir(q.machineConfig.StateDir),
	)
	q.process = p

	newCtx := monitor(ctx, p, q.machineConfig.OnFailure)
	return newCtx, p.Run()
}

func (q *CloudHypervisor) Config() types.MachineConfig {
	return q.machineConfig
}

func (q *CloudHypervisor) Stop() error {
	return process.New(process.WithStateDir(q.machineConfig.StateDir)).Stop()
}

func (q *CloudHypervisor) Clean() error {
	if q.machineConfig.StateDir != "" {
		return os.RemoveAll(q.machineConfig.StateDir)
	}
	return nil
}

func (q *CloudHypervisor) Alive() bool {
	return process.New(process.WithStateDir(q.machineConfig.StateDir)).IsAlive()
}

func (q *CloudHypervisor) Command(cmd string) (string, error) {
	return controller.SSHCommand(q, cmd)
}

func (q *CloudHypervisor) Screenshot() (string, error) {
	return "", errors.New("Screenshot is not implemented in cloud-hypervisor machine")
}

func (q *CloudHypervisor) DetachCD() error {
	return nil // Not applicable: cloud-hypervisor has no CD-ROM concept.
}

func (q *CloudHypervisor) ReceiveFile(src, dst string) error {
	return controller.ReceiveFile(q, src, dst)
}

func (q *CloudHypervisor) SendFile(src, dst, permissions string) error {
	return controller.SendFile(q, src, dst, permissions)
}
