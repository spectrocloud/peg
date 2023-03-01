package matcher

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"

	. "github.com/onsi/gomega"
)

type VM struct {
	machine    types.Machine
	cancelFunc context.CancelFunc // We call it when we `Destroy` the VM
	StateDir   string
}

func NewVM(m types.Machine, s string) VM {
	return VM{
		machine:  m,
		StateDir: s,
	}
}

func (vm VM) HasFile(s string) {
	machineHasFile(vm.machine, s)
}

func (vm VM) Sudo(s string) (string, error) {
	return machineSudo(vm.machine, s)
}

func (vm VM) Scp(s, d, permissions string) error {
	return machineScp(vm.machine, s, d, permissions)
}

func (vm VM) EventuallyConnects(t ...int) {
	machineEventuallyConnects(vm.machine, t...)
}

func (vm VM) Reboot(t ...int) {
	machineReboot(vm.machine, t...)
}

func (vm VM) DetachCD() error {
	return vm.machine.DetachCD()
}

func (vm VM) HasDir(s string) {
	machineHasDir(vm.machine, s)
}

func (vm VM) GatherLog(logPath string) {
	machineGatherLog(vm.machine, logPath)
}

func (vm VM) GatherAllLogs(services []string, logFiles []string) {
	machineGatherAllLogs(vm.machine, services, logFiles)
}

func (vm *VM) Start(ctx context.Context) (context.Context, error) {
	var newCtx context.Context
	newCtx, vm.cancelFunc = context.WithCancel(ctx)

	return vm.machine.Create(newCtx)
}

func (vm VM) Destroy(additionalCleanup func(vm VM)) error {
	if additionalCleanup != nil {
		additionalCleanup(vm)
	}
	if vm.cancelFunc != nil {
		vm.cancelFunc()
	}
	// Ensure the monitor function has enough time to read the closed context and
	// stop. This is to avoid the edge case in which we exit and the ticker runs
	// before the ctx.Done() is read, resulting in the Fail function to be called.
	time.Sleep(time.Second * 1)

	// Stop VM and cleanup state dir
	if vm.machine != nil {
		if err := vm.machine.Stop(); err != nil {
			return err
		}

		if err := vm.machine.Clean(); err != nil {
			return err
		}
	}

	return nil
}

var Machine types.Machine

func HasFile(s string) {
	machineHasFile(Machine, s)
}

func Reboot(t ...int) {
	machineReboot(Machine, t...)
}

func DetachCD() error {
	return machineDetachCD(Machine)
}

func HasDir(s string) {
	machineHasDir(Machine, s)
}

func EventuallyConnects(t ...int) {
	machineEventuallyConnects(Machine, t...)
}

func Sudo(c string) (string, error) {
	return machineSudo(Machine, c)
}

func Scp(s, d, permissions string) error {
	return machineScp(Machine, s, d, permissions)
}

// GatherAllLogs will try to gather as much info from the system as possible, including services, dmesg and os related info
func GatherAllLogs(services []string, logFiles []string) {
	machineGatherAllLogs(Machine, services, logFiles)
}

// GatherLog will try to scp the given log from the machine to a local file
func GatherLog(logPath string) {
	machineGatherLog(Machine, logPath)
}

func machineGatherLog(m types.Machine, logPath string) {
	machineSudo(m, "chmod 777 "+logPath)
	fmt.Printf("Trying to get file: %s\n", logPath)

	scpClient := controller.NewSCPClient(m)
	defer scpClient.Close()

	err := scpClient.Connect()
	if err != nil {
		fmt.Println("Couldn't establish a connection to the remote server ", err)
		return
	}

	baseName := filepath.Base(logPath)
	_ = os.Mkdir("logs", 0755)

	f, _ := os.Create(fmt.Sprintf("logs/%s", baseName))
	// Close the file after it has been copied
	// Close client connection after the file has been copied
	defer scpClient.Close()
	defer f.Close()

	ctx, can := context.WithTimeout(context.Background(), 2*time.Minute)
	defer can()
	err = scpClient.CopyFromRemote(ctx, f, logPath)
	if err != nil {
		fmt.Printf("Error while copying file: %s\n", err.Error())
		return
	}
	// Change perms so its world readable
	_ = os.Chmod(fmt.Sprintf("logs/%s", baseName), 0666)
	fmt.Printf("File %s copied!\n", baseName)
}

func machineHasFile(m types.Machine, s string) {
	out, err := m.Command("if [ -f " + s + " ]; then echo ok; else echo wrong; fi")
	Expect(err).ToNot(HaveOccurred())
	Expect(out).Should(Equal("ok\n"))
}

func machineSudo(m types.Machine, c string) (string, error) {
	// Write the command to a file to avoid quote escaping issues
	t, err := os.CreateTemp("", "tmpcmd")
	if err != nil {
		return "", errors.Wrap(err, "creating temporary file")
	}
	defer os.RemoveAll(t.Name())

	os.WriteFile(t.Name(), []byte(c), 0755)

	// "/run" should be writable in the kairos VM
	remoteFilePath := path.Join("/run", filepath.Base(t.Name()))
	err = m.SendFile(t.Name(), remoteFilePath, "0755")
	if err != nil {
		return "", errors.Wrap(err, "copying the tmp file into the VM")
	}

	result, err := m.Command(fmt.Sprintf(`sudo /bin/sh %s`, remoteFilePath))
	if err != nil {
		return result, errors.Wrapf(err, "executing the tmp script containing the command: %s", c)
	}

	_, err = m.Command(fmt.Sprintf(`sudo rm %s`, remoteFilePath))
	if err != nil {
		return result, errors.Wrap(err, "deleting temporary file")
	}

	return result, nil
}

func machineScp(m types.Machine, s, d, permissions string) error {
	return m.SendFile(s, d, permissions)
}

func machineEventuallyConnects(m types.Machine, t ...int) {
	dur := 360
	if len(t) > 0 {
		dur = t[0]
	}
	Eventually(func() string {
		out, _ := m.Command("echo ping")
		return out
	}, time.Duration(time.Duration(dur)*time.Second), time.Duration(5*time.Second)).Should(Equal("ping\n"))
}

func machineReboot(m types.Machine, t ...int) {
	machineSudo(m, "reboot") //nolint:errcheck
	time.Sleep(1 * time.Minute)
	timeout := 750
	if len(t) != 0 {
		timeout = t[0]
	}
	machineEventuallyConnects(m, timeout)
}

func machineDetachCD(m types.Machine) error {
	return m.DetachCD()
}

func machineHasDir(m types.Machine, s string) {
	out, err := m.Command("if [ -d " + s + " ]; then echo ok; else echo wrong; fi")
	Expect(err).ToNot(HaveOccurred())
	Expect(out).Should(Equal("ok\n"))
}

func machineGatherAllLogs(m types.Machine, services []string, logFiles []string) {
	// services
	for _, ser := range services {
		out, err := machineSudo(m, fmt.Sprintf("journalctl -u %s -o short-iso >> /run/%s.log", ser, ser))
		if err != nil {
			fmt.Printf("Error getting journal for service %s: %s\n", ser, err.Error())
			fmt.Printf("Output from command: %s\n", out)
		}
		machineGatherLog(m, fmt.Sprintf("/run/%s.log", ser))
	}

	// log files
	for _, file := range logFiles {
		machineGatherLog(m, file)
	}

	// dmesg
	out, err := machineSudo(m, "dmesg > /run/dmesg")
	if err != nil {
		fmt.Printf("Error getting dmesg : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	machineGatherLog(m, "/run/dmesg")

	// grab full journal
	out, err = machineSudo(m, "journalctl -o short-iso > /run/journal.log")
	if err != nil {
		fmt.Printf("Error getting full journalctl info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	machineGatherLog(m, "/run/journal.log")

	// uname
	out, err = machineSudo(m, "uname -a > /run/uname.log")
	if err != nil {
		fmt.Printf("Error getting uname info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	machineGatherLog(m, "/run/uname.log")

	// disk info
	out, err = machineSudo(m, "lsblk -a >> /run/disks.log")
	if err != nil {
		fmt.Printf("Error getting disk info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	out, err = machineSudo(m, "blkid >> /run/disks.log")
	if err != nil {
		fmt.Printf("Error getting disk info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	machineGatherLog(m, "/run/disks.log")

	// Grab users
	machineGatherLog(m, "/etc/passwd")
	// Grab system info
	machineGatherLog(m, "/etc/os-release")
}
