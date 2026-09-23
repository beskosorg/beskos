package sms

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/awssign"
)

// SNSSender publishes SMS directly to a phone number through Amazon SNS.
// SigV4 signing is shared with the email provider. Transactional SMS prioritizes MFA delivery.

type SNSSender struct {
	region    string
	accessKey string
	secretKey string
	senderID  string
	http      *http.Client
	now       func() time.Time // injectable in tests
}

func NewSNS(region, accessKey, secretKey, senderID string) *SNSSender {
	return &SNSSender{
		region: region, accessKey: accessKey, secretKey: secretKey, senderID: senderID,
		http: &http.Client{Timeout: 15 * time.Second},
		now:  time.Now,
	}
}

// publishForm builds the body (SNS Query API) -- separate for tests.
func publishForm(phone, message, senderID string) url.Values {
	form := url.Values{}
	form.Set("Action", "Publish")
	form.Set("Version", "2010-03-31")
	form.Set("PhoneNumber", phone)
	form.Set("Message", message)
	form.Set("MessageAttributes.entry.1.Name", "AWS.SNS.SMS.SMSType")
	form.Set("MessageAttributes.entry.1.Value.DataType", "String")
	form.Set("MessageAttributes.entry.1.Value.StringValue", "Transactional")
	if senderID != "" {
		form.Set("MessageAttributes.entry.2.Name", "AWS.SNS.SMS.SenderID")
		form.Set("MessageAttributes.entry.2.Value.DataType", "String")
		form.Set("MessageAttributes.entry.2.Value.StringValue", senderID)
	}
	return form
}

func (s *SNSSender) Send(ctx context.Context, phone, message string) error {
	body := []byte(publishForm(phone, message, s.senderID).Encode())
	endpoint := "https://sns." + s.region + ".amazonaws.com/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	awssign.SignRequest(req, body, s.region, "sns", s.accessKey, s.secretKey, s.now())
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("sns publish: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sns publish: HTTP %d: %s", resp.StatusCode, out)
	}
	return nil
}
