package provisioning

// Saying WHAT a step is waiting for, and shouting when the wait stops being plausible.
//
// A step that waits on the outside world answers "not yet" both when the wait is legitimate and
// when the condition will never be met. The two are indistinguishable from the log, and the
// difference is the whole story: one is a customer who has not got to it, the other is a bug on
// our side wearing a customer's clothes. This is law 2.9, and it exists because a domain sat in
// dns_pending for over an hour with every record published and visible worldwide -- the verifier
// was looking up a DKIM record whose contents an earlier bug had deleted.

import (
	"log"
	"sync"
	"time"
)

// How long a wait stays unremarkable. DNS propagates in minutes and the relay verifies in minutes;
// past this, "still waiting" is a claim worth doubting.
const patience = 20 * time.Minute

var waits struct {
	sync.Mutex
	since map[string]time.Time
	loud  map[string]bool
}

// waiting records that a domain is still waiting on `what`, naming the `target` it is looking for,
// and complains once the wait outlives our patience.
func waiting(domain, what, target string) {
	waits.Lock()
	defer waits.Unlock()
	if waits.since == nil {
		waits.since, waits.loud = map[string]time.Time{}, map[string]bool{}
	}
	key := domain + ":" + what
	start, seen := waits.since[key]
	if !seen {
		waits.since[key] = time.Now()
		log.Printf("provisioning: %s waiting on %s (%s)", domain, what, target)
		return
	}
	if waited := time.Since(start); waited > patience && !waits.loud[key] {
		waits.loud[key] = true
		log.Printf("provisioning: %s STILL waiting on %s after %s, looking for %q — if that is "+
			"published, the fault is ours, not the customer's", domain, what, waited.Round(time.Minute), target)
	}
}

// waitingDone clears the record for a domain that got through, so a later wait starts fresh and a
// recovery is visible rather than merely silent.
func waitingDone(domain string) {
	waits.Lock()
	defer waits.Unlock()
	for _, what := range []string{"dns", "relay"} {
		key := domain + ":" + what
		if waits.loud[key] {
			log.Printf("provisioning: %s no longer waiting on %s", domain, what)
		}
		delete(waits.since, key)
		delete(waits.loud, key)
	}
}
