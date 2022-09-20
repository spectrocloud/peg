package types

type Machine interface {
	Config() MachineConfig
	Create() error
	Stop() error
	Clean() error
	CreateDisk(diskname, size string) error
	Command(cmd string) (string, error)
	ReceiveFile(src, dst string) error
	SendFile(src, dst, permissions string) error
}
