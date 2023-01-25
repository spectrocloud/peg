package peg_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestPeg(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Peg Suite")
}
