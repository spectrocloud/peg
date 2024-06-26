package machine

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/spectrocloud/peg/internal/utils"
	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"
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
	if out, err := utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" poweroff`, v.machineConfig.ID)); err != nil {
		return errors.Wrap(err, out)
	}
	if out, err := utils.SH(fmt.Sprintf(`VBoxManage unregistervm --delete "%s"`, v.machineConfig.ID)); err != nil {
		return errors.Wrap(err, out)
	}
	if err := os.RemoveAll(v.machineConfig.StateDir); err != nil {
		return err
	}
	//utils.SH(fmt.Sprintf("rm -rf ~/VirtualBox\\ VMs/%s", ID))
	return nil
}

func (v *VBox) CreateDisk(diskname, size string) error {
	_, err := utils.SH(fmt.Sprintf("VBoxManage createmedium disk --filename %s --size %s", filepath.Join(v.machineConfig.StateDir, diskname), size))
	return err
}

func (v *VBox) Create(ctx context.Context) (context.Context, error) {
	out, err := utils.SH(fmt.Sprintf("VBoxManage createvm --name %[1]s --uuid %[1]s --register", v.machineConfig.ID))
	if err != nil {
		return ctx, fmt.Errorf("while creating VM: %w - %s", err, out)
	}

	out, err = utils.SH(fmt.Sprintf("VBoxManage modifyvm %s --memory %s --cpus %s", v.machineConfig.ID, v.machineConfig.Memory, v.machineConfig.CPU))
	if err != nil {
		return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
	}

	out, err = utils.SH(fmt.Sprintf(`VBoxManage modifyvm %s --nic1 nat --boot1 disk --boot2 dvd --natpf1 "guestssh,tcp,,%s,,22"`, v.machineConfig.ID, v.machineConfig.SSH.Port))
	if err != nil {
		return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
	}

	out, err = utils.SH(fmt.Sprintf(`VBoxManage storagectl "%s" --name "sata controller" --add sata --portcount 2 --hostiocache off`, v.machineConfig.ID))
	if err != nil {
		return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
	}

	driveSizes := v.driveSizes()
	userDrives := v.machineConfig.Drives
	if v.machineConfig.AutoDriveSetup && len(userDrives) == 0 {
		for i, s := range driveSizes {
			err := v.CreateDisk(fmt.Sprintf("%s-%d.vdi", v.machineConfig.ID, i), s)
			if err != nil {
				return ctx, err
			}
			userDrives = append(userDrives, filepath.Join(v.machineConfig.StateDir, fmt.Sprintf("%s-%d.vdi", v.machineConfig.ID, i)))
		}
	}

	totalDrives := 0
	for _, d := range userDrives {
		totalDrives++
		out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s-%d" --storagectl "sata controller" --port %d --device 0 --type hdd --medium %s`, v.machineConfig.ID, totalDrives-1, totalDrives, d))
		if err != nil {
			return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
		}
	}

	if v.machineConfig.ISO != "" {
		totalDrives++
		out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port %d --device 0 --type dvddrive --medium %s`, v.machineConfig.ID, totalDrives-1, v.machineConfig.ISO))
		if err != nil {
			return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
		}
	}

	if v.machineConfig.DataSource != "" {
		totalDrives++
		out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port %d --device 0 --type dvddrive --medium %s`, v.machineConfig.ID, totalDrives-1, v.machineConfig.DataSource))
		if err != nil {
			return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
		}
	}

	out, err = utils.SH(fmt.Sprintf(`VBoxManage startvm "%s" --type headless`, v.machineConfig.ID))
	if err != nil {
		return ctx, fmt.Errorf("while set VM: %w - %s", err, out)
	}

	return ctx, nil // TODO: Nothing monitors the vm process. The context won't be "Done" if it exits
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

func (v *VBox) Command(cmd string) (string, error) {
	return controller.SSHCommand(v, cmd)
}

func (v *VBox) ReceiveFile(src, dst string) error {
	return controller.ReceiveFile(v, src, dst)
}

func (v *VBox) SendFile(src, dst, permissions string) error {
	return controller.SendFile(v, src, dst, permissions)
}

func (v *VBox) driveSizes() []string {
	if len(v.machineConfig.DriveSizes) != 0 {
		return v.machineConfig.DriveSizes
	}

	return []string{types.DefaultDriveSize}
}
