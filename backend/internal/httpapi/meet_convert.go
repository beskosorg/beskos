package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// convertToPDF asks the Document Server (OnlyOffice) to convert an office file (pptx/ppt/odp/…)
// to PDF. sourceURL must be reachable BY THE DS (filesHostURL + signed route). Returns the URL of
// the generated PDF (on the DS itself, temporary) — the caller downloads it via reachable() and
// stores it in MinIO.
//
// ConvertService can be asynchronous; with async:false the DS can still respond "converting"
// (percent<100) for large files, so we do a short poll with the SAME key.
func (o *onlyoffice) convertToPDF(ctx context.Context, sourceURL, filetype, key, title string) (string, error) {
	deadline := time.Now().Add(45 * time.Second)
	for {
		payload := map[string]any{
			"async": false, "filetype": filetype, "outputtype": "pdf",
			"key": key, "title": title, "url": sourceURL,
		}
		tok, err := o.jwtSign(payload)
		if err != nil {
			return "", fmt.Errorf("sign convert: %w", err)
		}
		payload["token"] = tok
		body, _ := json.Marshal(payload)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.internalURL+"/ConvertService.ashx", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("convert request: %w", err)
		}
		var out struct {
			EndConvert *bool   `json:"endConvert"`
			FileURL    string  `json:"fileUrl"`
			Percent    float64 `json:"percent"`
			Error      *int    `json:"error"`
		}
		derr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if derr != nil {
			return "", fmt.Errorf("convert decode: %w", derr)
		}
		if out.Error != nil {
			return "", fmt.Errorf("convert error code %d", *out.Error)
		}
		if out.EndConvert != nil && *out.EndConvert && out.FileURL != "" {
			return out.FileURL, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("convert timeout (percent=%v)", out.Percent)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}
