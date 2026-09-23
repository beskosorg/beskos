package provisioning

import (
	"context"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Self-hosted domains use Stalwart's own delivery and DKIM provisioning.
type relayConfig struct{}

func (m *Machine) usesRelay() bool                                           { return false }
func (m *Machine) verifyRelayIdentity(context.Context, string) (bool, error) { return true, nil }
func (m *Machine) prepareRelayZone(_ context.Context, _ db.Domain, zone string) (string, error) {
	return zone, nil
}
