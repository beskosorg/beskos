package provisioning

// Getting the MTA-STS certificate issued BEFORE anyone needs it.
//
// The policy host serves every customer domain with a certificate issued on demand, during the TLS
// handshake, the first time somebody asks for that name. Issuance means a round trip to Let's
// Encrypt and a challenge: it takes seconds, not milliseconds.
//
// WHY THAT MATTERS ENOUGH TO WARRANT CODE: the first thing a customer does after publishing their
// records is check them -- often with our own free audit, whose TLS handshake gives up after four
// seconds. Without this, the very first look at a correctly configured domain reports MTA-STS as
// broken, and the customer concludes the product does not work. The mail itself would be fine; the
// impression would not be.
//
// Best-effort by design. A failure here means the certificate is issued later, on the first real
// request, exactly as it would have been anyway -- so nothing about provisioning depends on it and
// nothing about it can fail a domain.

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// warmMTASTS asks the policy host for this domain's policy once, so the certificate exists before
// the customer, or a sending server, looks for it.
//
// Does nothing unless a policy host is configured, which also keeps tests off the network: a fake
// domain would otherwise be resolved and dialled for real on every run.
func (m *Machine) warmMTASTS(ctx context.Context, domain string) {
	if domain == "" || m.mtaStsHost == "" {
		return
	}
	// Long enough for a full ACME issuance, which is the whole point of the call. It runs in the
	// worker's provisioning loop, once per domain, so the wait costs nothing anyone is waiting on.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	url := "https://mta-sts." + domain + "/.well-known/mta-sts.txt"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second, Transport: warmTransport()}).Do(req)
	if err != nil {
		// Expected while the customer's CNAME has not propagated yet. Logged rather than retried:
		// the next fetch of the policy issues the certificate regardless.
		log.Printf("mta-sts warm-up for %s: %v (the certificate will be issued on first use)", domain, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	log.Printf("mta-sts warm-up for %s: HTTP %d", domain, resp.StatusCode)
}

// warmTransport sends the warm-up straight to the policy service over the internal network when
// MTA_STS_INTERNAL_ADDR names one ("host:port"), and over the public internet otherwise.
//
// WHY THE DETOUR EXISTS: the policy host publishes port 443 on ONE node, and a container elsewhere
// in the cluster cannot reach that host port -- the first warm-up in production failed with
// "dial tcp 185.232.71.20:443: i/o timeout" while the very same URL answered from outside. Only the
// dial address changes: the request keeps the customer's hostname, so SNI still asks for their
// certificate and the response is still verified against it.
func warmTransport() http.RoundTripper {
	addr := os.Getenv("MTA_STS_INTERNAL_ADDR")
	if addr == "" {
		return http.DefaultTransport
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
		},
	}
}
