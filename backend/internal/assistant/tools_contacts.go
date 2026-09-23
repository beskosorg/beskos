package assistant

import (
	"context"
	"encoding/json"
	"fmt"
)

// ContactHit is a matched contact, the minimum for the assistant to decide/disambiguate a
// recipient.
type ContactHit struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company,omitempty"`
}

// ContactShare shares one of the user's contacts with a COLLEAGUE in the same tenant: copies it
// to the colleague's address book. Both are resolved by e-mail; the colleague must be in the
// tenant (§6.2).
type ContactShare func(ctx context.Context, id Identity, contactEmail, toEmail string) (ContactShareResult, error)

type ContactShareResult struct {
	Contact string `json:"contact"` // name of the shared contact
	To      string `json:"to"`      // name/e-mail of the colleague who received it
}

// ContactSearch searches the AUTHENTICATED user's contacts. Only from the Identity (§6.2).
type ContactSearch func(ctx context.Context, id Identity, query string, limit int) ([]ContactHit, error)

// NewContact is a contact to create in the user's address book.
type NewContact struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Company string `json:"company"`
}

// ContactCreate creates a contact with the SAME queries as the interface (§5: no new SQL).
type ContactCreate func(ctx context.Context, id Identity, c NewContact) (ContactHit, error)

type contactSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func contactCreateTools(create ContactCreate) []Tool {
	return []Tool{
		{
			Name: ToolContactsCreate,
			Description: "Add a contact to the user's address book (name, and optionally email, phone, " +
				"company). Internal to the user's own contacts — nothing is sent to anyone. Use it when " +
				"the user asks to save/add someone.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Full name of the contact."},
					"email": {"type": "string"},
					"phone": {"type": "string"},
					"company": {"type": "string"}
				},
				"required": ["name"]
			}`),
			Writes: true,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var c NewContact
				if err := decodeArgs(raw, &c); err != nil {
					return Result{}, err
				}
				if c.Name == "" {
					return Result{}, fmt.Errorf("the contact name is required")
				}
				hit, err := create(ctx, id, c)
				if err != nil {
					return Result{}, fmt.Errorf("contact creation failed: %w", err)
				}
				return Result{Summary: fmt.Sprintf("%s added to contacts", hit.Name), Data: hit}, nil
			},
		},
	}
}

func contactShareTools(share ContactShare) []Tool {
	return []Tool{
		{
			Name: ToolContactsShare,
			Description: "Share one of the user's contacts with a COLLEAGUE in the same organisation: " +
				"a copy of the contact is added to that colleague's address book. Give the contact's " +
				"email (the person whose card to share) and the colleague's email (who receives it). " +
				"Resolve both with contacts.search first if you only have names. Only colleagues in the " +
				"organisation can receive it; nothing is sent outside.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"contact_email": {"type": "string", "description": "E-mail of the contact whose card to share."},
					"to_email": {"type": "string", "description": "E-mail of the colleague who receives the contact."}
				},
				"required": ["contact_email", "to_email"]
			}`),
			Writes:  true,
			Confirm: true, // shares one person's data with another → needs approval
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					ContactEmail string `json:"contact_email"`
					ToEmail      string `json:"to_email"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if a.ContactEmail == "" || a.ToEmail == "" {
					return Result{}, fmt.Errorf("both the contact and the colleague e-mail are required")
				}
				res, err := share(ctx, id, a.ContactEmail, a.ToEmail)
				if err != nil {
					return Result{}, fmt.Errorf("contact share failed: %w", err)
				}
				return Result{Summary: fmt.Sprintf("shared %s with %s", res.Contact, res.To), Data: res}, nil
			},
		},
	}
}

func contactTools(search ContactSearch) []Tool {
	return []Tool{
		{
			Name: ToolContactsSearch,
			Description: "Search the organisation's colleagues AND the signed-in user's contacts by name " +
				"or email (colleagues come marked with company \"colleague\"). Use this to resolve WHO " +
				"a recipient is before writing or sending a message: if the user names someone (e.g. " +
				"\"Bob\") look them up here. If more than one contact matches, DO NOT guess — list the " +
				"matches (name, email, company) and ask the user which one. Only use an address that the " +
				"user confirmed or that is the single unambiguous match.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Name or email to look up (e.g. \"Bob\")."},
					"limit": {"type": "integer", "description": "How many matches to return (1-20, default 10)."}
				},
				"required": ["query"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a contactSearchArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				hits, err := search(ctx, id, a.Query, a.Limit)
				if err != nil {
					return Result{}, fmt.Errorf("contact search failed: %w", err)
				}
				res := Result{Summary: fmt.Sprintf("%d contacts", len(hits)), Data: hits}
				// Several candidates → buttons for the user to pick from, instead of typing. Value
				// carries name+e-mail so the model has no doubt which one was chosen.
				if len(hits) > 1 {
					for _, h := range hits {
						label := h.Name
						if h.Email != "" {
							label += " · " + h.Email
						}
						if h.Company != "" {
							label += " (" + h.Company + ")"
						}
						res.Choices = append(res.Choices, Choice{Label: label, Value: h.Name + " <" + h.Email + ">"})
					}
				}
				return res, nil
			},
		},
	}
}
