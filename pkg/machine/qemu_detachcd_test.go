package machine

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/spectrocloud/peg/pkg/machine/types"
)

// fakeMonitor serves one connection on the QEMU monitor socket the way QEMU
// does, records the command it gets, and answers with reply.
func fakeMonitor(stateDir, reply string) <-chan string {
	l, err := net.Listen("unix", filepath.Join(stateDir, "qemu-monitor.sock"))
	Expect(err).ToNot(HaveOccurred())

	got := make(chan string, 1)
	go func() {
		defer GinkgoRecover()
		defer l.Close()
		conn, err := l.Accept()
		Expect(err).ToNot(HaveOccurred())
		defer conn.Close()

		_, _ = conn.Write([]byte("QEMU 9.0.2 monitor - type 'help' for more information\r\n(qemu) "))
		line, err := bufio.NewReader(conn).ReadString('\n')
		Expect(err).ToNot(HaveOccurred())
		cmd := strings.TrimSpace(line)
		got <- cmd
		_, _ = conn.Write([]byte(cmd + "\r\n" + reply + "(qemu) "))
	}()
	return got
}

var _ = Describe("QEMU DetachCD", func() {
	var stateDir string

	BeforeEach(func() {
		var err error
		stateDir, err = os.MkdirTemp("", "peg-detachcd")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, stateDir)
	})

	It("ejects the scsi-cd drive the ISO is attached as", func() {
		got := fakeMonitor(stateDir, "")
		q := &QEMU{machineConfig: types.MachineConfig{
			StateDir:       stateDir,
			ISO:            "/tmp/kairos.iso",
			AutoDriveSetup: true,
			DriveSizes:     []string{"1000", "2000"},
		}}

		Expect(q.DetachCD()).To(Succeed())
		Expect(<-got).To(Equal("eject -f drv2"))
	})

	It("counts explicit drives before the ISO", func() {
		got := fakeMonitor(stateDir, "")
		q := &QEMU{machineConfig: types.MachineConfig{
			StateDir: stateDir,
			ISO:      "/tmp/kairos.iso",
			Drives:   []string{"/tmp/a.img"},
		}}

		Expect(q.DetachCD()).To(Succeed())
		Expect(<-got).To(Equal("eject -f drv1"))
	})

	It("returns the monitor error when the eject fails", func() {
		fakeMonitor(stateDir, "Error: Device 'drv0' not found\r\n")
		q := &QEMU{machineConfig: types.MachineConfig{
			StateDir: stateDir,
			ISO:      "/tmp/kairos.iso",
		}}

		err := q.DetachCD()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Device 'drv0' not found"))
	})
})
