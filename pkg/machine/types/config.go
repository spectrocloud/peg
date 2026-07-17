package types

import (
	"fmt"
	"io/ioutil"

	process "github.com/mudler/go-processmanager"
	"gopkg.in/yaml.v3"
)

const (
	DefaultDriveSize = "30000" // Mb
)

type SSH struct {
	User string `yaml:"user,omitempty"`
	Port string `yaml:"port,omitempty"`
	Pass string `yaml:"pass,omitempty"`
	Host string `yaml:"host,omitempty"` // defaults to 127.0.0.1; set to proxmox host for DNAT
}

type MachineConfig struct {
	StateDir    string `yaml:"state,omitempty"`
	Image       string `yaml:"image,omitempty"`
	ISO         string `yaml:"iso,omitempty"`
	ISOChecksum string `yaml:"isoChecksum,omitempty"`

	DataSource     string   `yaml:"datasource,omitempty"`
	Drives         []string `yaml:"drives,omitempty"`
	DriveSizes     []string `yaml:"driveSizes,omitempty"`
	AutoDriveSetup bool     `yaml:"auto_drive,omitempty"`
	ID             string   `yaml:"id,omitempty"`
	Memory         string   `yaml:"memory,omitempty"`
	CPU            string   `yaml:"cpu,omitempty"`
	Process        string   `yaml:"bin,omitempty"`
	Args           []string `yaml:"args,omitempty"`
	// only for qemu
	Display string `yaml:"display,omitempty"`

	CPUType string `yaml:"cpuType,omitempty"`

	// Network configuration
	DisableDefaultNetworking bool `yaml:"disable_default_networking,omitempty"`

	SSH    *SSH   `yaml:"ssh,omitempty"`
	Engine Engine `yaml:"engine,omitempty"`
	Arch   string `yaml:"arch,omitempty"`

	OnFailure func(*process.Process)

	Proxmox *ProxmoxConfig `yaml:"proxmox,omitempty"`
}

// ProxmoxConfig holds configuration specific to the Proxmox VE backend.
// Authentication: use either TokenID+TokenSecret (API token) or Username+Password (root login).
// Root login is required for VM features like custom QEMU args.
type ProxmoxConfig struct {
	APIURL      string `yaml:"apiURL,omitempty"`
	Node        string `yaml:"node,omitempty"`
	TokenID     string `yaml:"tokenID,omitempty"`
	TokenSecret string `yaml:"tokenSecret,omitempty"`
	Username    string `yaml:"username,omitempty"` // e.g., "root@pam"
	Password    string `yaml:"password,omitempty"`
	Storage     string `yaml:"storage,omitempty"` // e.g., "local-lvm"
	Bridge      string `yaml:"bridge,omitempty"`  // SDN VNet bridge, e.g., "vnet1"
	Zone        string `yaml:"zone,omitempty"`    // SDN zone, e.g., "nat-zone"
	InsecureTLS bool   `yaml:"insecureTLS,omitempty"`
	ISOStorage  string `yaml:"isoStorage,omitempty"` // Proxmox storage for ISOs, default: "local"
}

type Engine string

const (
	VBox    Engine = "vbox"
	QEMU    Engine = "qemu"
	Docker  Engine = "docker"
	Proxmox Engine = "proxmox"
)

type MachineOption func(*MachineConfig) error

func DefaultMachineConfig() *MachineConfig {
	return &MachineConfig{
		AutoDriveSetup: true,
		SSH:            &SSH{},
		CPU:            "2",
		Memory:         "2048",
		Arch:           "x86_64",
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

func WithDisplay(display string) MachineOption {
	return func(mc *MachineConfig) error {
		if display != "" {
			mc.Display = display
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

func WithCPUType(cpu string) MachineOption {
	return func(mc *MachineConfig) error {
		if cpu != "" {
			mc.CPUType = cpu
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
			mc.Drives = append(mc.Drives, drive)
		}

		return nil
	}
}

func WithDriveSize(drivesize string) MachineOption {
	return func(mc *MachineConfig) error {
		if drivesize != "" {
			mc.DriveSizes = append(mc.DriveSizes, drivesize)
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

func WithArch(arch string) MachineOption {
	return func(mc *MachineConfig) error {
		if arch != "" {
			mc.Arch = arch
		}
		return nil
	}
}

// VBoxEngine sets the machine engine to VBox.
var VBoxEngine MachineOption = func(mc *MachineConfig) error {
	mc.Engine = VBox
	return nil
}

// QEMUEngine sets the machine engine to QEMU.
var QEMUEngine MachineOption = func(mc *MachineConfig) error {
	mc.Engine = QEMU
	return nil
}

// EnableAutoDriveSetup automatically setup a VM disk if nothing is specified.
var EnableAutoDriveSetup MachineOption = func(mc *MachineConfig) error {
	mc.AutoDriveSetup = true
	return nil
}

// DisableAutoDriveSetup disables automatic disk setup.
var DisableAutoDriveSetup MachineOption = func(mc *MachineConfig) error {
	mc.AutoDriveSetup = false
	return nil
}

// DisableDefaultNetworking disables the default -nic networking setup.
// This allows for custom network configuration without conflicts.
var DisableDefaultNetworking MachineOption = func(mc *MachineConfig) error {
	mc.DisableDefaultNetworking = true
	return nil
}

// EnableDefaultNetworking enables the default -nic networking setup (default behavior).
var EnableDefaultNetworking MachineOption = func(mc *MachineConfig) error {
	mc.DisableDefaultNetworking = false
	return nil
}

// ProxmoxEngine sets the machine engine to Proxmox.
var ProxmoxEngine MachineOption = func(mc *MachineConfig) error {
	mc.Engine = Proxmox
	if mc.Proxmox == nil {
		mc.Proxmox = &ProxmoxConfig{}
	}
	return nil
}

func WithSSHHost(host string) MachineOption {
	return func(mc *MachineConfig) error {
		if host != "" {
			mc.SSH.Host = host
		}
		return nil
	}
}

func WithProxmoxAPIURL(url string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if url != "" {
			mc.Proxmox.APIURL = url
		}
		return nil
	}
}

func WithProxmoxNode(node string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if node != "" {
			mc.Proxmox.Node = node
		}
		return nil
	}
}

func WithProxmoxTokenID(tokenID string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if tokenID != "" {
			mc.Proxmox.TokenID = tokenID
		}
		return nil
	}
}

func WithProxmoxTokenSecret(secret string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if secret != "" {
			mc.Proxmox.TokenSecret = secret
		}
		return nil
	}
}

func WithProxmoxStorage(storage string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if storage != "" {
			mc.Proxmox.Storage = storage
		}
		return nil
	}
}

func WithProxmoxBridge(bridge string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if bridge != "" {
			mc.Proxmox.Bridge = bridge
		}
		return nil
	}
}

func WithProxmoxISOStorage(isoStorage string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if isoStorage != "" {
			mc.Proxmox.ISOStorage = isoStorage
		}
		return nil
	}
}

func WithProxmoxZone(zone string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if zone != "" {
			mc.Proxmox.Zone = zone
		}
		return nil
	}
}

func WithProxmoxUsername(username string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if username != "" {
			mc.Proxmox.Username = username
		}
		return nil
	}
}

func WithProxmoxPassword(password string) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		if password != "" {
			mc.Proxmox.Password = password
		}
		return nil
	}
}

func WithProxmoxInsecureTLS(insecure bool) MachineOption {
	return func(mc *MachineConfig) error {
		if mc.Proxmox == nil {
			mc.Proxmox = &ProxmoxConfig{}
		}
		mc.Proxmox.InsecureTLS = insecure
		return nil
	}
}
