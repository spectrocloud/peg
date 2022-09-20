package matcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spectrocloud/peg/pkg/controller"
	"github.com/spectrocloud/peg/pkg/machine/types"

	. "github.com/onsi/gomega"
)

var Machine types.Machine

func HasDir(s string) {
	out, err := Machine.Command("if [ -d " + s + " ]; then echo ok; else echo wrong; fi")
	Expect(err).ToNot(HaveOccurred())
	Expect(out).Should(Equal("ok\n"))
}

func EventuallyConnects(t ...int) {
	dur := 360
	if len(t) > 0 {
		dur = t[0]
	}
	Eventually(func() string {
		out, _ := Machine.Command("echo ping")
		return out
	}, time.Duration(time.Duration(dur)*time.Second), time.Duration(5*time.Second)).Should(Equal("ping\n"))
}

func Sudo(c string) (string, error) {
	return Machine.Command(fmt.Sprintf(`sudo /bin/sh -c "%s"`, c))
}

// GatherAllLogs will try to gather as much info from the system as possible, including services, dmesg and os related info
func GatherAllLogs(services []string, logFiles []string) {
	// services
	for _, ser := range services {
		out, err := Sudo(fmt.Sprintf("journalctl -u %s -o short-iso >> /run/%s.log", ser, ser))
		if err != nil {
			fmt.Printf("Error getting journal for service %s: %s\n", ser, err.Error())
			fmt.Printf("Output from command: %s\n", out)
		}
		GatherLog(fmt.Sprintf("/run/%s.log", ser))
	}

	// log files
	for _, file := range logFiles {
		GatherLog(file)
	}

	// dmesg
	out, err := Sudo("dmesg > /run/dmesg")
	if err != nil {
		fmt.Printf("Error getting dmesg : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/dmesg")

	// grab full journal
	out, err = Sudo("journalctl -o short-iso > /run/journal.log")
	if err != nil {
		fmt.Printf("Error getting full journalctl info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/journal.log")

	// uname
	out, err = Sudo("uname -a > /run/uname.log")
	if err != nil {
		fmt.Printf("Error getting uname info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/uname.log")

	// disk info
	out, err = Sudo("lsblk -a >> /run/disks.log")
	if err != nil {
		fmt.Printf("Error getting disk info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	out, err = Sudo("blkid >> /run/disks.log")
	if err != nil {
		fmt.Printf("Error getting disk info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/disks.log")

	// Grab users
	GatherLog("/etc/passwd")
	// Grab system info
	GatherLog("/etc/os-release")
}

// GatherLog will try to scp the given log from the machine to a local file
func GatherLog(logPath string) {
	Sudo("chmod 777 " + logPath)
	fmt.Printf("Trying to get file: %s\n", logPath)

	scpClient := controller.NewSCPClient(Machine)
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
