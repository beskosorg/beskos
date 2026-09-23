package stalwart

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration test against a test Stalwart in a container (brief §10).
// Run with:
//   STALWART_TEST_URL=http://localhost:18080 STALWART_TEST_USER=admin \
//   STALWART_TEST_PWD=probe-secret go test ./internal/stalwart -run TestIntegration -v
// Without the envs, the test is skipped (doesn't break `go test ./...` in CI without a container).

func testClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("STALWART_TEST_URL")
	if url == "" {
		t.Skip("STALWART_TEST_URL not set; skipping integration test")
	}
	return New(url, os.Getenv("STALWART_TEST_USER"), os.Getenv("STALWART_TEST_PWD"))
}

func TestIntegrationProvisioning(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ok, err := c.IsBootstrapped(ctx)
	if err != nil {
		t.Fatalf("IsBootstrapped: %v", err)
	}
	if !ok {
		t.Skip("server in bootstrap mode; run Bootstrap()+restart before this test")
	}

	// Somewhat-unique name without depending on time.Now() in the domain (short base32 of unix nano).
	domain := "it-" + time.Now().Format("150405") + ".test"

	// autoDKIM=true: the integration test wants the full zone, including the DKIM record.
	domID, err := c.CreateDomain(ctx, domain, "tenant_id=integration", true)
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	t.Logf("domain created id=%s", domID)

	// DNS zonefile should contain essential records.
	zone, err := c.GetDomainDNSZone(ctx, domID)
	if err != nil {
		t.Fatalf("GetDomainDNSZone: %v", err)
	}
	// Guaranteed at create time. The DKIM TXT (_domainkey) does NOT appear immediately
	// in the zonefile of a freshly created domain (signature generation isn't instant);
	// the DNS screen should poll until the DKIM record shows up. See DIVERGENCES §D4.
	for _, want := range []string{"MX", "spf1", "DMARC1"} {
		if !contains(zone, want) {
			t.Errorf("dnsZoneFile does not contain %q. zone=%s", want, zone)
		}
	}

	acctID, err := c.CreateAccount(ctx, AccountInput{
		Login:       "joao",
		DomainID:    domID,
		DisplayName: "John Integration",
		Password:    "correct-horse-battery-staple-9273",
		QuotaBytes:  5 << 30,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	t.Logf("account created id=%s", acctID)

	// Teardown in the correct order (account → domain) because of referential integrity.
	if err := c.DeleteAccount(ctx, acctID); err != nil {
		t.Errorf("DeleteAccount: %v", err)
	}
	if err := c.DeleteDomain(ctx, domID); err != nil {
		t.Errorf("DeleteDomain: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
