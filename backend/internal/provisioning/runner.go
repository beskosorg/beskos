package provisioning

import (
	"context"
	"log"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Lister is the subset of db.Queries the Runner uses to find pending work.
type Lister interface {
	ListDomainsInState(ctx context.Context, arg db.ListDomainsInStateParams) ([]db.Domain, error)
}

// Runner sweeps domains in non-terminal states and advances each one transition per tick.
// Errors on one domain don't stop the others (failure isolation). The worker calls Tick in a loop.
type Runner struct {
	machine *Machine
	lister  Lister
	batch   int32
}

func NewRunner(m *Machine, l Lister) *Runner {
	return &Runner{machine: m, lister: l, batch: 50}
}

// Tick processes one round. Returns how many transitions occurred (useful for logs/tests).
func (r *Runner) Tick(ctx context.Context) int {
	advanced := 0
	for _, state := range ActiveStates {
		domains, err := r.lister.ListDomainsInState(ctx, db.ListDomainsInStateParams{
			ProvisionState: state, Limit: r.batch,
		})
		if err != nil {
			log.Printf("provisioning: list state %s: %v", state, err)
			continue
		}
		for _, d := range domains {
			changed, err := r.machine.AdvanceOnce(ctx, d)
			if err != nil {
				// Error (probably transient): log it and try again next tick.
				log.Printf("provisioning: domain %s (%s): %v", d.Domain, d.ProvisionState, err)
				continue
			}
			if changed {
				advanced++
				log.Printf("provisioning: domain %s advanced from %s", d.Domain, d.ProvisionState)
			}
		}
	}
	return advanced
}
