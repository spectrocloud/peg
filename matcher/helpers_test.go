package matcher

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/spectrocloud/peg/pkg/machine"
)

var _ = Describe("VM.HardPowerCycle", func() {
	Context("when the backend is not QEMU", func() {
		It("returns an informative error and the original context", func() {
			vm := VM{machine: &machine.Docker{}}
			ctx, err := vm.HardPowerCycle(context.Background(), 60)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HardPowerCycle requires QEMU backend"))
			Expect(ctx).NotTo(BeNil())
		})
	})
})
