package types

import "context"

type Machine interface {
	Config() MachineConfig
	Create(ctx context.Context) error
	Stop() error
	Clean() error
	CreateDisk(diskname, size string) error
	Command(cmd string) (string, error)
	ReceiveFile(src, dst string) error
	SendFile(src, dst, permissions string) error
}
