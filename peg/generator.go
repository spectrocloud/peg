package peg

import (
	"context"
	"os"
	"sync"

	logging "github.com/ipfs/go-log"
	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	"github.com/spectrocloud/peg/internal/utils"

	"github.com/spectrocloud/peg/matcher"
)

func runOp(op OpBlock) {
	if op.EventuallyConnect != 0 {
		log.Infof("Running EventuallyConnect(%d)", op.EventuallyConnect)
		matcher.EventuallyConnects(op.EventuallyConnect)
	}
	if len(op.SendFile) > 0 {
		log.Infof("Running SendFile(%+v)", op.SendFile)
		err := matcher.Machine.SendFile(op.SendFile["src"], op.SendFile["dst"], op.SendFile["permission"])
		Expect(err).ToNot(HaveOccurred())
	}
	if len(op.ReceiveFile) > 0 {
		log.Infof("Running ReceiveFile(%+v)", op.ReceiveFile)
		err := matcher.Machine.ReceiveFile(op.ReceiveFile["src"], op.ReceiveFile["dst"])
		Expect(err).ToNot(HaveOccurred())
	}
}

func runAssertion(a AssertionBlock) {
	// Run pre Ops
	for _, o := range a.PreOps {
		runOp(o)
	}

	var out string
	var err error

	if a.OnHost {
		out, err = utils.SH(a.Command)
	} else {
		out, err = matcher.Machine.Command(a.Command)
	}

	if a.Expect.ToFail {
		Expect(err).To(HaveOccurred(), out)
	} else {
		Expect(err).ToNot(HaveOccurred(), out)
	}

	if a.Expect.isContainSubString() {
		if a.Expect.hasOrConditions() {
			ors := []OmegaMatcher{ContainSubstring(a.Expect.ContainSubstring)}
			for _, or := range a.Expect.Or {
				ors = append(ors, ContainSubstring(or.ContainSubstring))
			}
			if a.Expect.Not {
				Expect(out).ToNot(Or(ors...))
			} else {
				Expect(out).To(Or(ors...))
			}
		} else if a.Expect.hasAndConditions() {
			ands := []OmegaMatcher{ContainSubstring(a.Expect.ContainSubstring)}
			for _, or := range a.Expect.And {
				ands = append(ands, ContainSubstring(or.ContainSubstring))
			}
			if a.Expect.Not {
				Expect(out).ToNot(And(ands...))
			} else {
				Expect(out).To(And(ands...))
			}
		} else {
			if a.Expect.Not {
				Expect(out).ToNot(ContainSubstring(a.Expect.ContainSubstring))
			} else {
				Expect(out).To(ContainSubstring(a.Expect.ContainSubstring))
			}
		}
	}

	if a.Expect.isEqual() {
		if a.Expect.hasOrConditions() {
			ors := []OmegaMatcher{Equal(a.Expect.Equal)}
			for _, or := range a.Expect.Or {
				ors = append(ors, Equal(or.Equal))
			}
			if a.Expect.Not {
				Expect(out).ToNot(Or(ors...))
			} else {
				Expect(out).To(Or(ors...))
			}
		} else {
			if a.Expect.Not {
				Expect(out).ToNot(Equal(a.Expect.ContainSubstring))
			} else {
				Expect(out).To(Equal(a.Expect.ContainSubstring))
			}
		}
		if a.Expect.Not {
			Expect(out).ToNot(Equal(a.Expect.Equal))
		} else {
			Expect(out).To(Equal(a.Expect.Equal))
		}
	}

	for _, o := range a.PostOps {
		runOp(o)
	}
}

var logOutline = logging.Logger("test-preview")

// Generates test suites from a peg file.
func Generate(c *Config) error {
	logOutline.Info("Testsuite outline")

	BeforeSuite(func() {
		logOutline.Info("Machine creation")

		_, err := matcher.Machine.Create(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	AfterSuite(
		func() {
			err := matcher.Machine.Stop()
			Expect(err).ToNot(HaveOccurred())
			if c.Clean {
				err = matcher.Machine.Clean()
				Expect(err).ToNot(HaveOccurred())
			}
		},
	)

	logOutline.Infof("(!!) Tests found: %d", len(c.Tests))

	for _, t := range c.Tests {
		logOutline.Infof("-> Test spec '%s' ( label: %s )", t.Describe, t.Label)

		Describe(t.Describe, Label(t.Label), func() {
			for context, assertions := range t.Assertion {
				logOutline.Infof("--> Context: %s", context)
				Context(context, func() {
					for i := range assertions {
						a := assertions[i]
						a.Show(logOutline)
						It(a.Describe, func() {
							runAssertion(a)
						})
					}
				})
			}
		})
	}

	return nil
}

// Failer returns a simple fails that exists on failure.
func NewFailer() *Failer {
	return &Failer{}
}

// SyncedFailer returns a thread-safe failer that collects
// a failure and makes it accessible for later inspection.
func NewSyncedFailer() *Syncfailer {
	return &Syncfailer{}
}

func (f *Syncfailer) Fail() {
	f.Lock()
	defer f.Unlock()
	f.failed = true
}

func (f *Syncfailer) Failed() bool {
	f.Lock()
	defer f.Unlock()
	return f.failed
}

type Syncfailer struct {
	sync.Mutex

	failed bool
}

type Failer struct {
}

func (f Failer) Fail() {
	os.Exit(1)
}
