package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"os"
	"strings"
	"time"
)

// NetDNSVerifier confirms publication by checking real DNS resolution: the domain needs at least
// one MX record and one DKIM TXT record (_domainkey). This is a minimal verification; SPF, DMARC,
// and MTA-STS can be added later.
type NetDNSVerifier struct {
	resolver *net.Resolver
}

// NewNetDNSVerifier builds the verifier. It asks DNS_RESOLVER ("host:port") when set, and the
// system resolver otherwise.
//
// WHY IT MATTERS WHICH RESOLVER ANSWERS: "this record does not exist" is cached for the zone's SOA
// minimum, and plenty of providers publish a whole day there. The worker asks every five seconds,
// so it always asks BEFORE the customer has published -- and then keeps being told the records are
// missing long after they exist. Our own resolver caps negative answers at a minute; somebody
// else's does whatever the customer's DNS provider asked for.
func NewNetDNSVerifier() *NetDNSVerifier {
	addr := os.Getenv("DNS_RESOLVER")
	if addr == "" {
		return &NetDNSVerifier{resolver: net.DefaultResolver}
	}
	return &NetDNSVerifier{resolver: &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
	}}
}

func (v *NetDNSVerifier) Verify(ctx context.Context, domain, zone string) (bool, error) {
	mx, err := v.resolver.LookupMX(ctx, domain)
	if err != nil || len(mx) == 0 {
		return false, nil // not published yet (not a fatal error)
	}
	// Looks for the expected DKIM selector from Stalwart's zonefile.
	selector := dkimHost(zone, domain)
	if selector == "" {
		return len(mx) > 0, nil // no known selector yet: MX alone is enough for now
	}
	txts, err := v.resolver.LookupTXT(ctx, selector)
	if err != nil || len(txts) == 0 {
		return false, nil
	}
	for _, t := range txts {
		if strings.Contains(t, "DKIM1") || strings.Contains(t, "k=") {
			return true, nil
		}
	}
	return false, nil
}

// dkimHost extracts the host of the _domainkey record from the zonefile (e.g. "sel._domainkey.domain.").
//
// It insists on a line that carries the key itself, not merely the name. A record written across
// several lines has a header with the name and nothing else, and accepting that header meant
// waiting for a record whose contents had been removed -- silently, because a lookup that finds
// nothing is indistinguishable here from a customer who has not published yet.
func dkimHost(zone, domain string) string {
	for _, line := range strings.Split(zone, "\n") {
		if strings.Contains(line, "_domainkey."+domain) && strings.Contains(line, "DKIM1") {
			return strings.TrimSuffix(strings.Fields(line)[0], ".")
		}
	}
	return ""
}

// AlwaysVerified skips DNS verification (dev/local, where test domains don't resolve).
// Enabled via config (PROVISION_SKIP_DNS_VERIFY=true). NEVER use in production.
type AlwaysVerified struct{}

func (AlwaysVerified) Verify(context.Context, string, string) (bool, error) { return true, nil }

// genInitialSecret generates a strong, DETERMINISTIC secret per domain (idempotency on retry).
// High entropy (derived from the domain's UUID) to pass Stalwart's strength checker.
// The tenant admin changes the password afterward.
func genInitialSecret(seed string) string {
	sum := sha256.Sum256([]byte("mailin-postmaster-v1:" + seed))
	return "Pm9!" + base64.RawURLEncoding.EncodeToString(sum[:18])
}
