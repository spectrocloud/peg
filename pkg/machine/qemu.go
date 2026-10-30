package machine

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"context"

	process "github.com/mudler/go-processmanager"
	"github.com/spectrocloud/peg/internal/utils"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

type QEMU struct {
	machineConfig types.MachineConfig
	process       *process.Process
}

// findQEMUBinary searches for qemu-system-x86_64 in common installation paths
func findQEMUBinary(arch string) (string, error) {
	qemuBinary := fmt.Sprintf("qemu-system-%s", arch)
	// Common paths where QEMU might be installed
	commonPaths := []string{
		"/home/linuxbrew/.linuxbrew/bin/", // Homebrew on Linux
		"/usr/local/bin/",                 // Manual install
		"/usr/bin/",                       // System package
		"/opt/homebrew/bin/",              // Homebrew on macOS ARM
		"/usr/local/homebrew/bin/",        // Homebrew on macOS Intel
	}

	// Check each common path first
	for _, path := range commonPaths {
		if _, err := os.Stat(filepath.Join(path, qemuBinary)); err == nil {
			return filepath.Join(path, qemuBinary), nil
		}
	}

	// Fallback to searching in PATH
	path, err := exec.LookPath(qemuBinary)
	if err != nil {
		return "", fmt.Errorf("%s not found in common paths or PATH: %w", qemuBinary, err)
	}

	return path, nil
}

func (q *QEMU) Create(ctx context.Context) (context.Context, error) {
	log.Info("Create qemu machine")

	driveSizes := q.driveSizes()
	userDrives := q.machineConfig.Drives
	if q.machineConfig.AutoDriveSetup && len(userDrives) == 0 {
		for i, s := range driveSizes {
			filename := fmt.Sprintf("%s-%d.img", q.machineConfig.ID, i)
			err := q.CreateDisk(filename, s)
			if err != nil {
				return ctx, fmt.Errorf("creating disk with size %s: %w", s, err)
			}
			userDrives = append(userDrives, filepath.Join(q.machineConfig.StateDir, filename))
		}
	}

	genDrives := func(m types.MachineConfig) []string {
		var allDrives []string
		scsiAdded := false
		id := 0

		// User disks: bootindex 1..N (highest priority)
		for i, d := range userDrives {
			driveID := fmt.Sprintf("drv%d", id)
			id++

			allDrives = append(allDrives,
				"-drive", fmt.Sprintf("if=none,id=%s,file=%s", driveID, d),
				"-device", fmt.Sprintf("virtio-blk-pci,drive=%s,bootindex=%d", driveID, i+1),
			)
		}

		// If we have any CDROMs we want them as /dev/srX => add a virtio-scsi controller once
		addSCSIIfNeeded := func() {
			if !scsiAdded {
				// create a virtio SCSI controller
				allDrives = append(allDrives,
					"-device", "virtio-scsi-pci,id=scsi0",
				)
				scsiAdded = true
			}
		}

		// Primary ISO -> appear as /dev/srX (scsi-cd)
		if m.ISO != "" {
			addSCSIIfNeeded()
			driveID := q.isoDriveID()
			id++

			allDrives = append(allDrives,
				"-drive", fmt.Sprintf("if=none,id=%s,media=cdrom,file=%s", driveID, m.ISO),
				"-device", fmt.Sprintf("scsi-cd,drive=%s,bus=scsi0.0,bootindex=50", driveID),
			)
		}

		// Data-source ISO -> also /dev/srX
		if m.DataSource != "" {
			addSCSIIfNeeded()
			driveID := fmt.Sprintf("drv%d", id)
			id++

			allDrives = append(allDrives,
				"-drive", fmt.Sprintf("if=none,id=%s,media=cdrom,file=%s", driveID, m.DataSource),
				"-device", fmt.Sprintf("scsi-cd,drive=%s,bus=scsi0.0,bootindex=60", driveID),
			)
		}

		return allDrives
	}

	var processName string
	if q.machineConfig.Process != "" {
		processName = q.machineConfig.Process
	} else {
		var err error
		processName, err = findQEMUBinary(q.machineConfig.Arch)
		if err != nil {
			return ctx, fmt.Errorf("failed to find QEMU binary: %w", err)
		}
	}

	log.Infof("Starting VM with %s [ Memory: %s, CPU: %s ]", processName, q.machineConfig.Memory, q.machineConfig.CPU)
	for _, d := range userDrives {
		log.Infof("HD at %s, state directory at %s", d, q.machineConfig.StateDir)
	}
	if q.machineConfig.ISO != "" {
		log.Infof("ISO at %s", q.machineConfig.ISO)
	}

	display := "-nographic"

	// this could be something like
	// -vga qxl -spice port=5900,disable-ticketing,addr=127.0.0.1"
	// see qemu docs for more info
	if q.machineConfig.Display != "" {
		display = q.machineConfig.Display
	}

	// Enable qemu monitor to enable screendump (used in `Screenshot()`):
	opts := []string{
		"-m", q.machineConfig.Memory,
		"-smp", fmt.Sprintf("cores=%s", q.machineConfig.CPU),
		"-rtc", "base=utc,clock=rt",
		"-monitor", fmt.Sprintf("unix:%s,server,nowait", q.monitorSockFile()),
		"-device", "virtio-serial",
	}

	// Add default networking unless disabled
	if !q.machineConfig.DisableDefaultNetworking {
		opts = append(opts, "-nic", fmt.Sprintf("user,hostfwd=tcp::%s-:22", q.machineConfig.SSH.Port))
	}

	opts = append(opts, strings.Split(display, " ")...)

	if q.machineConfig.CPUType != "" {
		opts = append(opts, "-cpu", q.machineConfig.CPUType)
	} else if q.machineConfig.Arch == "aarch64" {
		// For aarch64, set a default CPU type if not specified
		opts = append(opts, "-cpu", "max")
	}

	opts = append(opts, q.machineConfig.Args...)

	if q.machineConfig.Arch == "aarch64" {
		// For aarch64, we need to specify machine type and firmware
		opts = append(opts,
			"-machine", "virt,accel=tcg,acpi=on,gic-version=2",
		)
	}

	// Use QEMU boot order "dc" (disk, then cdrom). This works in conjunction with
	// per-drive bootindex values: 1-N for disks, 50 for ISO, and 60 for the
	// cloud-init/DataSource drive. The boot order ensures disks are considered
	// first, while bootindex controls the precedence among multiple devices of
	// the same type.
	opts = append(opts, "-boot", "order=dc,menu=on")

	log.Infof("Creating QEMU machine with args: %s", strings.Join(append(opts, genDrives(q.machineConfig)...), " "))

	qemu := process.New(
		process.WithName(processName),
		process.WithArgs(opts...),
		process.WithArgs(genDrives(q.machineConfig)...),
		process.WithStateDir(q.machineConfig.StateDir),
	)

	q.process = qemu

	newCtx := monitor(ctx, qemu, q.machineConfig.OnFailure)
	return newCtx, qemu.Run()
}

func (q *QEMU) Config() types.MachineConfig {
	return q.machineConfig
}

// qemu monitor: https://qemu-project.gitlab.io/qemu/system/monitor.html
// nice explanation of how it works: https://unix.stackexchange.com/a/476617
// unix sockets with golang: https://dev.to/douglasmakey/understanding-unix-domain-sockets-in-golang-32n8
func (q *QEMU) Screenshot() (string, error) {
	conn, err := net.Dial("unix", q.monitorSockFile())
	if err != nil {
		return "", err
	}
	defer conn.Close()

	// Create a temp file name
	f, err := os.CreateTemp("", "qemu-screenshot-*.png")
	if err != nil {
		return "", err
	}
	f.Close()
	os.Remove(f.Name())

	cmd := fmt.Sprintf("screendump %s\r\n", f.Name())
	n, err := fmt.Fprint(conn, cmd)
	if err != nil {
		return "", err
	}

	if n != len(cmd) {
		return "", fmt.Errorf("didn't send the full command (%d out of %d bytes)", n, len(cmd))
	}

	// If there is nothing for more than a second, stop
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return "", err
	}

	// It seems that the screendump image.png command doesn't have any effect
	// until we read the data from the socket. I would expect reading the data to
	// be irrelevant but after trial and errors, this seems to be necessary for some reason.
	for {
		b := make([]byte, 1024)
		if _, err := conn.Read(b); err != nil {
			break
		}
	}

	return f.Name(), nil
}

func (q *QEMU) Stop() error {
	return process.New(process.WithStateDir(q.machineConfig.StateDir)).Stop()
}

func (q *QEMU) Clean() error {
	if q.machineConfig.StateDir != "" {
		return os.RemoveAll(q.machineConfig.StateDir)
	}
	return nil
}

func (q *QEMU) Alive() bool {
	return process.New(process.WithStateDir(q.machineConfig.StateDir)).IsAlive()
}

func (q *QEMU) CreateDisk(diskname, size string) error {
	if err := os.MkdirAll(q.machineConfig.StateDir, os.ModePerm); err != nil {
		return err
	}
	out, err := utils.SH(fmt.Sprintf("qemu-img create -f qcow2 %s %s", filepath.Join(q.machineConfig.StateDir, diskname), size))
	if err != nil {
		return fmt.Errorf("%s : %w", out, err)
	}

	return nil
}

func (q *QEMU) Command(cmd string) (string, error) {
	return controller.SSHCommand(q, cmd)
}

func (q *QEMU) DetachCD() error {
	conn, err := net.Dial("unix", q.monitorSockFile())
	if err != nil {
		return err
	}
	defer conn.Close()

	// The ISO is attached as a scsi-cd whose drive id is set in Create
	// (see genDrives), so eject that id rather than a legacy IDE name.
	cmd := fmt.Sprintf("eject -f %s\r\n", q.isoDriveID())
	n, err := fmt.Fprint(conn, cmd)
	if err != nil {
		return err
	}

	if n != len(cmd) {
		return fmt.Errorf("didn't send the full command (%d out of %d bytes)", n, len(cmd))
	}

	// If there is nothing for more than a second, stop
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}

	// Read the monitor reply until it goes quiet, so a failed eject surfaces
	// as an error instead of leaving the ISO attached.
	var reply strings.Builder
	for {
		b := make([]byte, 1024)
		n, err := conn.Read(b)
		reply.Write(b[:n])
		if err != nil {
			break
		}
	}

	for _, line := range strings.Split(reply.String(), "\n") {
		if i := strings.Index(line, "Error:"); i >= 0 {
			return fmt.Errorf("ejecting %s: %s", q.isoDriveID(), strings.TrimSpace(line[i:]))
		}
	}

	return nil
}

// isoDriveID returns the drive id genDrives gives the primary ISO: the user
// drives take drv0..drvN-1, so the ISO is drvN.
func (q *QEMU) isoDriveID() string {
	n := len(q.machineConfig.Drives)
	if q.machineConfig.AutoDriveSetup && n == 0 {
		n = len(q.driveSizes())
	}
	return fmt.Sprintf("drv%d", n)
}

func (q *QEMU) ReceiveFile(src, dst string) error {
	return controller.ReceiveFile(q, src, dst)
}

func (q *QEMU) SendFile(src, dst, permissions string) error {
	return controller.SendFile(q, src, dst, permissions)
}

func (q *QEMU) monitorSockFile() string {
	return path.Join(q.machineConfig.StateDir, "qemu-monitor.sock")
}

// Converts the user's drive sizes (which are Mb as strings) to the qemu format.
// https://qemu.readthedocs.io/en/latest/tools/qemu-img.html#cmdoption-qemu-img-arg-create
func (q *QEMU) driveSizes() []string {
	sizes := []string{}

	for _, s := range q.machineConfig.DriveSizes {
		sizes = append(sizes, fmt.Sprintf("%sM", s))
	}

	if len(sizes) == 0 {
		sizes = append(sizes, fmt.Sprintf("%sM", types.DefaultDriveSize))
	}

	return sizes
}
