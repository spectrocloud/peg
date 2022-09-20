package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bramvdbogaerde/go-scp"
	"github.com/spectrocloud/peg/pkg/machine/types"
	"golang.org/x/crypto/ssh"
)

// NewSCPClient returns a SCP client associated to the machine
func NewSCPClient(m types.Machine) scp.Client {
	sshConfig, dialAddr := sshConfig(m)

	return scp.NewClientWithTimeout(dialAddr, sshConfig, 10*time.Second)
}

// NewClient returns a new ssh client associated to a machine
func NewClient(m types.Machine) (*ssh.Client, *ssh.Session, error) {
	sshConfig, dialAddr := sshConfig(m)

	client, err := SSHDialTimeout("tcp", dialAddr, sshConfig, 30*time.Second)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}

func sshConfig(m types.Machine) (*ssh.ClientConfig, string) {
	sshConfig := &ssh.ClientConfig{
		User:    m.Config().SSH.User,
		Auth:    []ssh.AuthMethod{ssh.Password(m.Config().SSH.Pass)},
		Timeout: 30 * time.Second, // max time to establish connection
	}

	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	return sshConfig, fmt.Sprintf("127.0.0.1:%s", m.Config().SSH.Port)
}

func ReceiveFile(m types.Machine, src, dst string) error {
	scpClient := NewSCPClient(m)

	if err := scpClient.Connect(); err != nil {
		return err
	}
	defer scpClient.Close()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	err = scpClient.CopyFromRemote(context.Background(), f, src)
	if err != nil {
		return err
	}
	return nil
}

func SendFile(m types.Machine, src, dst, permission string) error {
	scpClient := NewSCPClient(m)
	defer scpClient.Close()

	if err := scpClient.Connect(); err != nil {
		return err
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}

	defer scpClient.Close()
	defer f.Close()

	if err := scpClient.CopyFile(context.Background(), f, dst, permission); err != nil {
		return err
	}
	return nil
}

func SSHCommand(m types.Machine, cmd string) (string, error) {
	client, session, err := NewClient(m)
	if err != nil {
		return "", err
	}
	defer client.Close()
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(out), err
	}

	return string(out), err
}
