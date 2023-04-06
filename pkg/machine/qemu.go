package machine

import (
	"fmt"
	"net"
	"os"
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

func (q *QEMU) Create(ctx context.Context) (context.Context, error) {
	log.Info("Create qemu machine")

	drive := q.machineConfig.Drive
	if q.machineConfig.AutoDriveSetup && q.machineConfig.Drive == "" {
		err := q.CreateDisk(fmt.Sprintf("%s.img", q.machineConfig.ID), "40g")
		if err != nil {
			return ctx, err
		}
		drive = filepath.Join(q.machineConfig.StateDir, fmt.Sprintf("%s.img", q.machineConfig.ID))
	}

	genDrives := func(m types.MachineConfig) []string {
		drives := []string{}
		if m.ISO != "" {
			drives = append(drives, "-drive", fmt.Sprintf("if=ide,media=cdrom,file=%s", m.ISO))
		}
		if m.DataSource != "" {
			drives = append(drives, "-drive", fmt.Sprintf("if=ide,media=cdrom,file=%s", m.DataSource))
		}
		if drive != "" {
			drives = append(drives, "-drive", fmt.Sprintf("if=virtio,media=disk,file=%s", drive))
		}

		return drives
	}

	processName := "/usr/bin/qemu-system-x86_64"
	if q.machineConfig.Process != "" {
		processName = q.machineConfig.Process
	}

	log.Infof("Starting VM with %s [ Memory: %s, CPU: %s ]", processName, q.machineConfig.Memory, q.machineConfig.CPU)
	log.Infof("HD at %s, state directory at %s", drive, q.machineConfig.StateDir)
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
		"-device", "virtio-serial", "-nic", fmt.Sprintf("user,hostfwd=tcp::%s-:22", q.machineConfig.SSH.Port),
	}

	opts = append(opts, strings.Split(display, " ")...)

	if q.machineConfig.CPUType != "" {
		opts = append(opts, "-cpu", q.machineConfig.CPUType)
	}

	opts = append(opts, q.machineConfig.Args...)

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
		if err := q.Stop(); err != nil {
			return err
		}
		fmt.Println("Cleaning", q.machineConfig.StateDir)
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
	_, err := utils.SH(fmt.Sprintf("qemu-img create -f qcow2 %s %s", filepath.Join(q.machineConfig.StateDir, diskname), size))
	return err
}

func (q *QEMU) Command(cmd string) (string, error) {
	return controller.SSHCommand(q, cmd)
}

func (q *QEMU) DetachCD() error {
	fmt.Println("Warning! DetachCD not implemented in QEMU")
	return nil
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
