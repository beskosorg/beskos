package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Provisioning operations, mapped from the product's domain lifecycle to the real JMAP methods
// on v0.16.4.

// CreateDomain creates a domain (equivalent to the old type:domain). With autoDKIM=true Stalwart
// manages and signs DKIM (dkimManagement=Automatic). With autoDKIM=false (relay/white-label mode)
// DKIM stays Manual — Stalwart does NOT sign, leaving the relay (SES BYODKIM) as the sole signer,
// avoiding a duplicated DKIM-Signature header. Returns the stalwart_id.
func (c *Client) CreateDomain(ctx context.Context, name, description string, autoDKIM bool) (string, error) {
	create := map[string]any{
		"name":        name,
		"isEnabled":   true,
		"description": description, // we store "tenant_id=<n>" here, as in the brief
	}
	if !autoDKIM {
		create["dkimManagement"] = map[string]any{"@type": "Manual"}
	}
	args := map[string]any{
		"create": map[string]any{"d1": create},
	}
	raw, err := c.call(ctx, "x:Domain/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode Domain/set: %w", err)
	}
	id, err := firstCreatedID(&r)
	if err == nil {
		return id, nil
	}
	// Idempotency: if the domain already exists in Stalwart (primaryKeyViolation) — server
	// recreated, bootstrap's default domain, or a provisioning retry — adopt the existing one
	// instead of failing in a loop. This makes the generic flow (customer configures the same
	// domain) robust.
	if isPrimaryKeyViolation(r.NotCreated) {
		existing, lookupErr := c.domainIDByName(ctx, name)
		if lookupErr != nil {
			return "", fmt.Errorf("adopt existing domain %q: %w", name, lookupErr)
		}
		return existing, nil
	}
	return "", err
}

// isPrimaryKeyViolation reports whether some notCreated was because the object already exists
// (duplicate key).
func isPrimaryKeyViolation(nc map[string]json.RawMessage) bool {
	for _, v := range nc {
		var e struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(v, &e) == nil && e.Type == "primaryKeyViolation" {
			return true
		}
	}
	return false
}

// domainIDByName looks up a domain's stalwart_id by name (x:Domain/get returns all of them when
// no ids are passed). Used in the idempotent adoption of an already-existing domain.
func (c *Client) domainIDByName(ctx context.Context, name string) (string, error) {
	raw, err := c.call(ctx, "x:Domain/get", map[string]any{"properties": []string{"name"}})
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode Domain/get: %w", err)
	}
	for _, d := range g.List {
		if strings.EqualFold(d.Name, name) {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("domain %q not found in stalwart", name)
}

// AccountInput holds the minimal fields to create an individual mailbox.
type AccountInput struct {
	Login       string // becomes the local-part; there is NO "email" field in 0.16 (address = Login@domain)
	DomainID    string // domain's stalwart_id (or "#d1" for a same-request ref)
	DisplayName string
	Password    string // goes through the strength checker (zxcvbn); weak passwords are rejected
	QuotaBytes  int64
}

// CreateAccount creates an individual mailbox (type User). Returns stalwart_account_id.
func (c *Client) CreateAccount(ctx context.Context, in AccountInput) (string, error) {
	acct := map[string]any{
		"@type":       "User",
		"name":        in.Login,
		"domainId":    in.DomainID,
		"description": in.DisplayName,
		// credentials/aliases/quotas are MAPS ("0","1",...), not arrays (DIVERGENCES §D4).
		"credentials": map[string]any{
			"0": map[string]any{"@type": "Password", "secret": in.Password},
		},
	}
	if in.QuotaBytes > 0 {
		acct["quotas"] = map[string]any{"maxDiskQuota": in.QuotaBytes}
	}
	args := map[string]any{"create": map[string]any{"u1": acct}}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return "", err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode Account/set: %w", err)
	}
	return firstCreatedID(&r)
}

// Alias is an extra address that delivers into a mailbox: local part + domain id IN STALWART.
type Alias struct {
	Local    string // "noreply"
	DomainID string // domain id in Stalwart (domains.stalwart_id)
}

// SetAccountAliases replaces an account's entire alias list. It always sends the WHOLE list: our
// database is the source of truth, which makes the operation idempotent — repeating it neither
// duplicates entries nor leaves gaps.
//
// FORMAT, confirmed against the Stalwart source (crates/registry/src/schema/structs.rs):
// `UserAccount.aliases` is `List<EmailAlias>`, which in JSON means an indexed MAP (`{"0":…,"1":…}`)
// whose VALUES are OBJECTS `{name, domainId, enabled}` — not plain-text addresses. An alias is
// described as a mailbox: local part + domain, with no `email` field. Sending plain strings gets
// `invalidPatch: "Invalid value type for object"`, which is exactly what we hit in production.
func (c *Client) SetAccountAliases(ctx context.Context, accountID string, aliases []Alias) error {
	m := make(map[string]any, len(aliases))
	for i, a := range aliases {
		m[strconv.Itoa(i)] = map[string]any{
			"name":     a.Local,
			"domainId": a.DomainID,
			"enabled":  true, // by default it would be false: the alias would exist but not deliver
		}
	}
	args := map[string]any{
		"update": map[string]any{accountID: map[string]any{"aliases": m}},
	}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Account/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		b, _ := json.Marshal(r.NotUpdated)
		sent, _ := json.Marshal(m)
		return fmt.Errorf("stalwart notUpdated: %s (we sent aliases=%s)", string(b), string(sent))
	}
	return nil
}

// SetAccountPassword changes a mailbox's password (x:Account/set update credentials/0/secret).
// See DIVERGENCES §D4. The password goes through Stalwart's strength checker (zxcvbn).
func (c *Client) SetAccountPassword(ctx context.Context, accountID, newPassword string) error {
	args := map[string]any{
		"update": map[string]any{
			accountID: map[string]any{"credentials/0/secret": newPassword},
		},
	}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Account/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		b, _ := json.Marshal(r.NotUpdated)
		err := fmt.Errorf("stalwart notUpdated: %s", string(b))
		if isWeakSecret(r.NotUpdated) {
			return fmt.Errorf("%w: %v", ErrWeakPassword, err)
		}
		return err
	}
	return nil
}

// DeleteDomain / DeleteAccount — destroy. Watch out for referential integrity:
// destroying a domain with linked accounts/DKIM fails with objectIsLinked (DIVERGENCES §D4).
func (c *Client) DeleteAccount(ctx context.Context, id string) error {
	_, err := c.call(ctx, "x:Account/set", map[string]any{"destroy": []string{id}})
	return err
}

func (c *Client) DeleteDomain(ctx context.Context, id string) error {
	_, err := c.call(ctx, "x:Domain/set", map[string]any{"destroy": []string{id}})
	return err
}

// GetDomainDNSZone returns the ready-made BIND zone snippet (dnsZoneFile) with MX/SPF/DKIM/DMARC/MTA-STS/…
// It's the source for the DNS screen in dns_pending state (§7). We parse this, we don't build it by hand.
func (c *Client) GetDomainDNSZone(ctx context.Context, domainID string) (string, error) {
	args := map[string]any{"ids": []string{domainID}, "properties": []string{"dnsZoneFile"}}
	raw, err := c.call(ctx, "x:Domain/get", args)
	if err != nil {
		return "", err
	}
	var g struct {
		List []struct {
			DNSZoneFile string `json:"dnsZoneFile"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return "", fmt.Errorf("decode Domain/get: %w", err)
	}
	if len(g.List) == 0 {
		return "", fmt.Errorf("stalwart: domain %s not found", domainID)
	}
	return g.List[0].DNSZoneFile, nil
}

// SuspendAccount blocks every way in without touching a single message: it removes the account's
// credentials. Mail keeps arriving, the mailbox keeps its contents, and IMAP, SMTP, JMAP and our
// own panel all stop accepting the password at once.
//
// WHY THIS AND NOT A FLAG: an account in Stalwart 0.16 has no "enabled" field -- the object carries
// name, quotas, roles, permissions, aliases and credentials, and nothing that says "off". Probed
// against a live account: removing the credentials map denies the next login immediately, and
// writing a new secret lets it back in immediately.
//
// Reactivating therefore needs a NEW password (SetAccountPassword) -- the old one is gone. That is
// not a workaround dressed up as a feature: reinstating a suspended person's old password is the
// wrong default anyway, since suspension usually means somebody left or was compromised.
func (c *Client) SuspendAccount(ctx context.Context, accountID string) error {
	args := map[string]any{
		"update": map[string]any{accountID: map[string]any{"credentials": map[string]any{}}},
	}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Account/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		b, _ := json.Marshal(r.NotUpdated)
		return fmt.Errorf("stalwart notUpdated: %s", string(b))
	}
	return nil
}

// RenameAccount changes the local part of an account, and with it the address it answers to.
//
// This is how a deleted mailbox stops accepting mail without a single message being destroyed. The
// account keeps everything it had; it simply no longer holds the address, so a sender to the old
// one is told the recipient does not exist -- which is the truth, and is what every other provider
// does. Renaming it back is the whole of "restore".
//
// Verified against a live account: emailAddress follows the name in both directions and the
// stored mail is untouched.
func (c *Client) RenameAccount(ctx context.Context, accountID, newLocal string) error {
	args := map[string]any{
		"update": map[string]any{accountID: map[string]any{"name": newLocal}},
	}
	raw, err := c.call(ctx, "x:Account/set", args)
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Account/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		b, _ := json.Marshal(r.NotUpdated)
		return fmt.Errorf("stalwart notUpdated: %s", string(b))
	}
	return nil
}
