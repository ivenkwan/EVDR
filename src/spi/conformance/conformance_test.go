package conformance_test

import (
	"testing"

	"github.com/ivenkwan/evdr/src/spi"
	"github.com/ivenkwan/evdr/src/spi/conformance"
	"github.com/ivenkwan/evdr/src/spi/conformance/memadapter"
)

// memHarness wires the suite to the in-memory reference adapter. Running the
// suite against two structurally different backends proves it is
// adapter-agnostic (TR-2.4) and guards against the suite encoding
// Nextcloud-specific assumptions.
type memHarness struct{}

func (memHarness) New(t *testing.T) (spi.RoomSPI, spi.TenantContext, func()) {
	t.Helper()
	return memadapter.New("tenant-a"), spi.TenantContext{
		TenantID: "tenant-a",
		Actor:    spi.Actor{Kind: spi.ActorInternalUser, ID: "operator-1", DisplayName: "Operator"},
	}, func() {}
}

// TestConformanceSuiteInMemoryAdapter runs the shared SPI conformance suite
// against the in-memory reference adapter.
func TestConformanceSuiteInMemoryAdapter(t *testing.T) {
	conformance.RunSuite(t, memHarness{})
}
