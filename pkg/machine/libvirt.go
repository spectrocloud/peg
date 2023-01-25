package machine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"context"

	"github.com/google/uuid"

	process "github.com/mudler/go-processmanager"
	"github.com/spectrocloud/peg/internal/utils"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

type Libvirt struct {
	machineConfig types.MachineConfig
	process       *process.Process
}

func (l *Libvirt) Create(ctx context.Context) error {
	log.Info("Create a machine with libvirt (virt-install)")

	drive := l.machineConfig.Drive
	if l.machineConfig.AutoDriveSetup && l.machineConfig.Drive == "" {
		err := l.CreateDisk(fmt.Sprintf("%s.img", l.machineConfig.ID), "40g")
		if err != nil {
			return err
		}
		drive = filepath.Join(l.machineConfig.StateDir, fmt.Sprintf("%s.img", l.machineConfig.ID))
	}

	vmName := uuid.New().String()
	vmFile := filepath.Join(l.machineConfig.StateDir, "vm_name")
	os.WriteFile(vmFile, []byte(vmName), 0744)

	genDrives := func(m types.MachineConfig) []string {
		drives := []string{}
		if m.ISO != "" {
			drives = append(drives, "--cdrom", m.ISO)
		}
		if m.DataSource != "" {
			drives = append(drives, "--cdrom", m.DataSource)
		}
		if drive != "" {
			drives = append(drives, "--disk", fmt.Sprintf("%s,format=qcow2,bus=virtio", drive))
		}

		return drives
	}

	processName := "/usr/bin/virt-install"
	if l.machineConfig.Process != "" {
		processName = l.machineConfig.Process
	}

	log.Infof("Starting VM with %s [ Memory: %s, CPU: %s ]", processName, l.machineConfig.Memory, l.machineConfig.CPU)
	log.Infof("HD at %s, state directory at %s", drive, l.machineConfig.StateDir)
	if l.machineConfig.ISO != "" {
		log.Infof("ISO at %s", l.machineConfig.ISO)
	}

	opts := []string{
		"--os-variant", "opensuse-factory", // TODO
		"--memory", l.machineConfig.Memory,
		"--vcpus", fmt.Sprintf("cores=%s", l.machineConfig.CPU),
		//"-rtc", "base=utc,clock=rt",
		"--graphics", "none",
		// "-device", "virtio-serial", TODO
		//"--serial", fmt.Sprintf("tcp,host=:%s,bind_host=:22", l.machineConfig.SSH.Port),
		//"--channel", "source.bind_host=:2222,source.mode=bind,target.type=guestfwd,target.address=:22",
		//"--console", "pty,target.type=virtio",
		//"--network", fmt.Sprintf("user,hostfwd=tcp::%s-:22", l.machineConfig.SSH.Port),

		fmt.Sprintf("--qemu-commandline='-nic user,hostfwd=tcp::%s-:22'", l.machineConfig.SSH.Port),

		// /usr/bin/qemu-system-x86_64 -m 2000 -smp cores=2 -rtc base=utc,clock=rt -nographic -device virtio-serial -nic user,hostfwd=tcp::42211-:22 -drive if=ide,media=cdrom,file=/home/dimitris/workspace/kairos/kcrypt-challenger/build/challenger.iso -drive if=virtio,media=disk,file=/tmp/peg778631851/OoMBBMOmlQ.img
		//"-nic", fmt.Sprintf("user,hostfwd=tcp::%s-:22", l.machineConfig.SSH.Port),
		"-n", vmName,
	}
	if l.machineConfig.CPUType != "" {
		opts = append(opts, "-cpu", l.machineConfig.CPUType)
	}

	opts = append(opts, l.machineConfig.Args...)

	virtinstall := process.New(
		process.WithName(processName),
		process.WithArgs(opts...),
		process.WithArgs(genDrives(l.machineConfig)...),
		process.WithStateDir(l.machineConfig.StateDir),
	)

	l.process = virtinstall

	l.machineConfig.OnFailure = func(p *process.Process) {
		eb, _ := os.ReadFile(p.StderrPath())
		ob, _ := os.ReadFile(p.StdoutPath())
		fmt.Println("Failed:")
		fmt.Printf("stderr:\n%s\n", string(eb))
		fmt.Printf("stdout:\n%s\n\n", string(ob))
	}
	monitor(ctx, virtinstall, l.machineConfig.OnFailure)

	return virtinstall.Run()
}

func (l *Libvirt) Config() types.MachineConfig {
	return l.machineConfig
}

func (l *Libvirt) Stop() error {
	vmFile := filepath.Join(l.machineConfig.StateDir, "vm_name")
	b, err := os.ReadFile(vmFile)
	if err != nil {
		return err
	}

	cmd := exec.Command("virsh", "destroy", string(b))
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("virsh", "undefine", string(b))
	return cmd.Run()
}

func (l *Libvirt) Clean() error {
	if l.machineConfig.StateDir != "" {
		fmt.Println("Cleaning", l.machineConfig.StateDir)
		return os.RemoveAll(l.machineConfig.StateDir)
	}
	return nil
}

func (l *Libvirt) Alive() bool {
	return process.New(process.WithStateDir(l.machineConfig.StateDir)).IsAlive()
}

func (l *Libvirt) CreateDisk(diskname, size string) error {
	os.MkdirAll(l.machineConfig.StateDir, os.ModePerm)
	_, err := utils.SH(fmt.Sprintf("qemu-img create -f qcow2 %s %s", filepath.Join(l.machineConfig.StateDir, diskname), size))
	return err
}

func (l *Libvirt) Command(cmd string) (string, error) {
	return controller.SSHCommand(l, cmd)
}

func (l *Libvirt) ReceiveFile(src, dst string) error {
	return controller.ReceiveFile(l, src, dst)
}

func (l *Libvirt) SendFile(src, dst, permissions string) error {
	return controller.SendFile(l, src, dst, permissions)
}
