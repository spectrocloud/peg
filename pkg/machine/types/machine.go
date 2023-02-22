package types

import "context"

type Machine interface {
	Config() MachineConfig
	Create(ctx context.Context) (context.Context, error)
	Stop() error
	Clean() error
	CreateDisk(diskname, size string) error
	Command(cmd string) (string, error)
	DetachCD() error
	ReceiveFile(src, dst string) error
	SendFile(src, dst, permissions string) error
}
