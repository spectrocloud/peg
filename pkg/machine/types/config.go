package types

import (
	"fmt"
	"io/ioutil"

	process "github.com/mudler/go-processmanager"
	"gopkg.in/yaml.v3"
)

type SSH struct {
	User string `yaml:"user,omitempty"`
	Port string `yaml:"port,omitempty"`
	Pass string `yaml:"pass,omitempty"`
}

type MachineConfig struct {
	StateDir    string `yaml:"state,omitempty"`
	Image       string `yaml:"image,omitempty"`
	ISO         string `yaml:"iso,omitempty"`
	ISOChecksum string `yaml:"isoChecksum,omitempty"`

	DataSource     string   `yaml:"datasource,omitempty"`
	Drive          string   `yaml:"drive,omitempty"`
	AutoDriveSetup bool     `yaml:"auto_drive,omitempty"`
	ID             string   `yaml:"id,omitempty"`
	Memory         string   `yaml:"memory,omitempty"`
	CPU            string   `yaml:"cpu,omitempty"`
	Process        string   `yaml:"bin,omitempty"`
	Args           []string `yaml:"args,omitempty"`

	SSH    *SSH   `yaml:"ssh,omitempty"`
	Engine Engine `yaml:"engine,omitempty"`

	OnFailure func(*process.Process)
}

type Engine string

const (
	VBox   Engine = "vbox"
	QEMU   Engine = "qemu"
	Docker Engine = "docker"
)

type MachineOption func(*MachineConfig) error

func DefaultMachineConfig() *MachineConfig {
	return &MachineConfig{
		AutoDriveSetup: true,
		SSH:            &SSH{},
		CPU:            "2",
		Memory:         "2048",
	}
}

func (m *MachineConfig) Apply(opts ...MachineOption) error {
	for _, o := range opts {
		if err := o(m); err != nil {
			return err
		}
	}
	return nil
}

func WithSSHPass(sshpass string) MachineOption {
	return func(mc *MachineConfig) error {
		if sshpass != "" {
			mc.SSH.Pass = sshpass
		}
		return nil
	}
}

func OnFailure(f func(*process.Process)) MachineOption {
	return func(mc *MachineConfig) error {
		mc.OnFailure = f
		return nil
	}
}

func WithMemory(mem string) MachineOption {
	return func(mc *MachineConfig) error {
		if mem != "" {
			mc.Memory = mem
		}
		return nil
	}
}

func WithDataSource(ds string) MachineOption {
	return func(mc *MachineConfig) error {
		if ds != "" {
			mc.DataSource = ds
		}
		return nil
	}
}

func WithCPU(cpu string) MachineOption {
	return func(mc *MachineConfig) error {
		if cpu != "" {
			mc.CPU = cpu
		}
		return nil
	}
}

func WithISOChecksum(iso string) MachineOption {
	return func(mc *MachineConfig) error {
		if iso != "" {
			mc.ISOChecksum = iso
		}

		return nil
	}
}

func WithImage(img string) MachineOption {
	return func(mc *MachineConfig) error {
		if img != "" {
			mc.Image = img
		}

		return nil
	}
}

func WithISO(iso string) MachineOption {
	return func(mc *MachineConfig) error {
		if iso != "" {
			mc.ISO = iso
		}

		return nil
	}
}

func WithDrive(drive string) MachineOption {
	return func(mc *MachineConfig) error {
		if drive != "" {
			mc.Drive = drive
		}

		return nil
	}
}

func FromFile(path string) MachineOption {
	return func(mc *MachineConfig) error {
		dat, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed reading %s: %w", path, err)
		}
		if err := yaml.Unmarshal(dat, mc); err != nil {
			return fmt.Errorf("failed unmarshalling %s: %w", path, err)
		}
		return nil
	}
}

func WithProcessName(proc string) MachineOption {
	return func(mc *MachineConfig) error {
		if proc != "" {
			mc.Process = proc
		}
		return nil
	}
}

func WithID(id string) MachineOption {
	return func(mc *MachineConfig) error {
		if id != "" {
			mc.ID = id
		}
		return nil
	}
}

func WithSSHPort(sshport string) MachineOption {
	return func(mc *MachineConfig) error {
		if sshport != "" {
			mc.SSH.Port = sshport
		}
		return nil
	}
}

func WithSSHUser(sshuser string) MachineOption {
	return func(mc *MachineConfig) error {
		if sshuser != "" {
			mc.SSH.User = sshuser
		}
		return nil
	}
}

func WithStateDir(dir string) MachineOption {
	return func(mc *MachineConfig) error {
		if dir != "" {
			mc.StateDir = dir
		}
		return nil
	}
}

// VBoxEngine sets the machine engine to VBox
var VBoxEngine MachineOption = func(mc *MachineConfig) error {
	mc.Engine = VBox
	return nil
}

// QEMUEngine sets the machine engine to QEMU
var QEMUEngine MachineOption = func(mc *MachineConfig) error {
	mc.Engine = QEMU
	return nil
}

// EnableAutoDriveSetup automatically setup a VM disk if nothing is specified
var EnableAutoDriveSetup MachineOption = func(mc *MachineConfig) error {
	mc.AutoDriveSetup = true
	return nil
}

// DisableAutoDriveSetup disables automatic disk setup
var DisableAutoDriveSetup MachineOption = func(mc *MachineConfig) error {
	mc.AutoDriveSetup = false
	return nil
}
