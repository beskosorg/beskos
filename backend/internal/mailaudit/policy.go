package mailaudit

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Fetching the MTA-STS policy, not merely believing the TXT record that advertises it.
//
// WHY THE RECORD IS NOT ENOUGH: _mta-sts announces that a policy exists at
// https://mta-sts.<domain>/.well-known/mta-sts.txt. Publishing the announcement is one DNS edit;
// serving the policy needs a host answering on that exact name with a valid certificate. The two
// come apart easily -- and a checker that reads only the record would tell somebody their MTA-STS is
// fine while no sender on the internet can read it. That is the kind of wrong answer this tool
// exists to stop giving.
//
// RFC 8461 FORBIDS REDIRECTS on this fetch, so following one would be reporting on a different
// document than the senders will read.

// SSRF: the hostname comes from whatever a stranger typed. The address is checked at DIAL time, not
// before, which is what closes DNS rebinding.
func blocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

var policyClient = &http.Client{
	Timeout: 6 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse // the RFC says no redirects; we report what it says
	},
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, err
			}
			for _, ip := range ips {
				if blocked(ip.IP) {
					return nil, &net.AddrError{Err: "blocked address", Addr: ip.IP.String()}
				}
			}
			return (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		// The certificate has to be valid FOR THAT NAME. Skipping verification here would be
		// checking something no real sender would accept.
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 4 * time.Second,
	},
}

// mtaStsCheck reports whether the policy is both announced and readable.
func mtaStsCheck(ctx context.Context, r *net.Resolver, domain string) Check {
	announced := txtCheck(ctx, r, "_mta-sts."+domain, "mta_sts", "v=STSv1")
	if announced.Status != Pass {
		return announced
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://mta-sts."+domain+"/.well-known/mta-sts.txt", nil)
	if err != nil {
		return Check{Key: "mta_sts", Status: Unknown}
	}
	resp, err := policyClient.Do(req)
	if err != nil {
		// Announced but unreadable. Warn and not Fail: senders simply carry on without a policy, so
		// nothing breaks -- but the protection the record promises does not exist.
		return Check{Key: "mta_sts", Status: Warn, Detail: "announced, but the policy could not be fetched"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Check{Key: "mta_sts", Status: Warn, Detail: "announced, but the policy answered " + resp.Status}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if !strings.Contains(strings.ToLower(string(body)), "version: stsv1") {
		return Check{Key: "mta_sts", Status: Warn, Detail: "announced, but what is served is not a policy"}
	}
	mode := "enforce"
	if strings.Contains(strings.ToLower(string(body)), "mode: testing") {
		mode = "testing"
	}
	return Check{Key: "mta_sts", Status: Pass, Detail: "policy served (" + mode + ")"}
}
