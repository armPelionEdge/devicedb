package devicedb_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestCompatibility(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "compatibility Suite")
}