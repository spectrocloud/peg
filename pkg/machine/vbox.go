package machine

import (
	"context"
	"fmt"
	"github.com/spectrocloud/peg/internal/utils"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"
	"io/ioutil"
	"os"
	"path/filepath"
	//. "github.com/onsi/ginkgo/v2"
	//. "github.com/onsi/gomega"
)

type VBox struct {
	machineConfig types.MachineConfig
}

func (v *VBox) Stop() error {
	return nil
}

func (v *VBox) Config() types.MachineConfig {
	return v.machineConfig
}

func (v *VBox) Clean() error {
	utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" poweroff`, v.machineConfig.ID))
	utils.SH(fmt.Sprintf(`VBoxManage unregistervm --delete "%s"`, v.machineConfig.ID))
	utils.SH(fmt.Sprintf(`VBoxManage closemedium disk "%s"`, filepath.Join(v.machineConfig.StateDir, v.machineConfig.Drive)))
	os.RemoveAll(v.machineConfig.StateDir)
	//utils.SH(fmt.Sprintf("rm -rf ~/VirtualBox\\ VMs/%s", ID))
	return nil
}

func (v *VBox) CreateDisk(diskname, size string) error {
	_, err := utils.SH(fmt.Sprintf("VBoxManage createmedium disk --filename %s --size %s", filepath.Join(v.machineConfig.StateDir, diskname), size))
	return err
}

func (v *VBox) Create(ctx context.Context) error {

	out, err := utils.SH(fmt.Sprintf("VBoxManage createvm --name %s --register", v.machineConfig.ID))
	if err != nil {
		return fmt.Errorf("while creating VM: %w - %s", err, out)
	}

	out, err = utils.SH(fmt.Sprintf("VBoxManage modifyvm %s --memory %s --cpus %s", v.machineConfig.ID, v.machineConfig.Memory, v.machineConfig.CPU))
	if err != nil {
		return fmt.Errorf("while set VM: %w - %s", err, out)
	}

	out, err = utils.SH(fmt.Sprintf(`VBoxManage modifyvm %s --nic1 nat --boot1 disk --boot2 dvd --natpf1 "guestssh,tcp,,%s,,22"`, v.machineConfig.ID, v.machineConfig.SSH.Port))
	if err != nil {
		return fmt.Errorf("while set VM: %w - %s", err, out)
	}

	out, err = utils.SH(fmt.Sprintf(`VBoxManage storagectl "%s" --name "sata controller" --add sata --portcount 2 --hostiocache off`, v.machineConfig.ID))
	if err != nil {
		return fmt.Errorf("while set VM: %w - %s", err, out)
	}

	drive := v.machineConfig.Drive
	if v.machineConfig.AutoDriveSetup && v.machineConfig.Drive == "" {
		err := v.CreateDisk(fmt.Sprintf("%s.vdi", v.machineConfig.ID), "30000")
		if err != nil {
			return err
		}
		drive = filepath.Join(v.machineConfig.StateDir, fmt.Sprintf("%s.vdi", v.machineConfig.ID))
	}

	if drive != "" {
		out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 0 --device 0 --type hdd --medium %s`, v.machineConfig.ID, drive))
		if err != nil {
			return fmt.Errorf("while set VM: %w - %s", err, out)
		}
	}

	if v.machineConfig.ISO != "" {
		out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 1 --device 0 --type dvddrive --medium %s`, v.machineConfig.ID, v.machineConfig.ISO))
		if err != nil {
			return fmt.Errorf("while set VM: %w - %s", err, out)
		}
	}

	if v.machineConfig.DataSource != "" {
		out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 2 --device 0 --type dvddrive --medium %s`, v.machineConfig.ID, v.machineConfig.DataSource))
		if err != nil {
			return fmt.Errorf("while set VM: %w - %s", err, out)
		}
	}

	out, err = utils.SH(fmt.Sprintf(`VBoxManage startvm "%s" --type headless`, v.machineConfig.ID))
	if err != nil {
		return fmt.Errorf("while set VM: %w - %s", err, out)
	}
	return nil

}

func (v *VBox) Screenshot() (string, error) {
	f, err := ioutil.TempFile("", "fff")
	if err != nil {
		return "", err
	}
	_, err = utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" screenshotpng "%s"`, v.machineConfig.ID, f.Name()))
	if err != nil {
		return "", err
	}
	return f.Name(), nil
}

func (v *VBox) DetachCD() error {
	_, err := utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 1 --device 0 --medium none`, v.machineConfig.ID))
	return err
}

func (v *VBox) Restart() error {
	_, err := utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" reset`, v.machineConfig.ID))
	return err
}

func (q *VBox) Command(cmd string) (string, error) {
	return controller.SSHCommand(q, cmd)
}

func (q *VBox) ReceiveFile(src, dst string) error {
	return controller.ReceiveFile(q, src, dst)
}

func (q *VBox) SendFile(src, dst, permissions string) error {
	return controller.SendFile(q, src, dst, permissions)
}
