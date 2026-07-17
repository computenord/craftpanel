package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

// ManagedComment marks records this panel created. Reconciliation only ever
// updates or deletes records carrying it; everything else in the zone is
// left untouched.
const ManagedComment = "managed by craftpanel"

// Cloudflare edits DNS records through the Cloudflare v4 API. The token
// needs Zone:Read and DNS:Edit for the zone holding the base domain.
type Cloudflare struct {
	Token string
	// BaseURL overrides the API endpoint in tests.
	BaseURL string
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfError       `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfSRVData struct {
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Port     int    `json:"port"`
	Target   string `json:"target"`
}

type cfRecord struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"`
	Name    string     `json:"name"`
	Content string     `json:"content"`
	Proxied *bool      `json:"proxied"`
	Comment string     `json:"comment"`
	Data    *cfSRVData `json:"data,omitempty"`
}

func (c *Cloudflare) do(ctx context.Context, method, path string, body, out any) error {
	base := c.BaseURL
	if base == "" {
		base = cloudflareAPI
	}
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var env cfEnvelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare: HTTP %d, unreadable response", res.StatusCode)
	}
	if !env.Success {
		if len(env.Errors) > 0 {
			return fmt.Errorf("cloudflare: %s (code %d)", env.Errors[0].Message, env.Errors[0].Code)
		}
		return fmt.Errorf("cloudflare: HTTP %d", res.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("cloudflare: parse result: %w", err)
		}
	}
	return nil
}

// findZone walks the domain's labels upward until a zone in this account
// matches, so "mc.example.com" finds the "example.com" zone.
func (c *Cloudflare) findZone(ctx context.Context, domain string) (string, error) {
	labels := strings.Split(domain, ".")
	for i := 0; i <= len(labels)-2; i++ {
		name := strings.Join(labels[i:], ".")
		var zones []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := c.do(ctx, http.MethodGet, "/zones?per_page=5&name="+url.QueryEscape(name), nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("no Cloudflare zone found for %q — does the API token cover this zone?", domain)
}

func (c *Cloudflare) listRecords(ctx context.Context, zone string) ([]cfRecord, error) {
	all := []cfRecord{}
	for page := 1; page <= 50; page++ {
		var recs []cfRecord
		path := fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", zone, page)
		if err := c.do(ctx, http.MethodGet, path, nil, &recs); err != nil {
			return nil, err
		}
		all = append(all, recs...)
		if len(recs) < 100 {
			break
		}
	}
	return all, nil
}

func recordPayload(r Record) map[string]any {
	p := map[string]any{"type": r.Type, "name": r.Name, "comment": ManagedComment, "ttl": 300}
	if r.Type == "SRV" {
		p["data"] = map[string]any{"priority": 0, "weight": 5, "port": r.Port, "target": r.Target}
	} else {
		p["content"] = r.Content
		p["proxied"] = false
	}
	return p
}

func recordMatches(e cfRecord, r Record) bool {
	if r.Type == "SRV" {
		return e.Data != nil && e.Data.Port == r.Port && strings.EqualFold(e.Data.Target, r.Target)
	}
	return strings.EqualFold(e.Content, r.Content) && (e.Proxied == nil || !*e.Proxied)
}

// Sync reconciles the zone holding domain against the desired records:
// creates what is missing, updates managed records that drifted and deletes
// managed records under the domain that are no longer wanted. Records
// without the managed comment are never modified; a desired record whose
// name is already taken by a foreign record is skipped with a warning.
func (c *Cloudflare) Sync(ctx context.Context, domain string, desired []Record) (Result, error) {
	var res Result
	zone, err := c.findZone(ctx, domain)
	if err != nil {
		return res, err
	}
	existing, err := c.listRecords(ctx, zone)
	if err != nil {
		return res, err
	}

	key := func(typ, name string) string { return typ + "|" + strings.ToLower(name) }
	want := map[string]Record{}
	for _, r := range desired {
		want[key(r.Type, r.Name)] = r
	}

	suffix := "." + strings.ToLower(domain)
	managed := map[string]cfRecord{}
	foreign := map[string]bool{}
	// Delete stale managed records first, so a record changing type
	// (A -> CNAME after a target change) does not collide with its
	// predecessor at the same name.
	for _, e := range existing {
		k := key(e.Type, e.Name)
		if e.Comment != ManagedComment {
			foreign[k] = true
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name), suffix) {
			// Managed by another craftpanel instance sharing the zone.
			continue
		}
		if _, ok := want[k]; !ok {
			if err := c.do(ctx, http.MethodDelete, "/zones/"+zone+"/dns_records/"+e.ID, nil, nil); err != nil {
				return res, err
			}
			res.Deleted++
			continue
		}
		managed[k] = e
	}

	for k, r := range want {
		if e, ok := managed[k]; ok {
			if recordMatches(e, r) {
				continue
			}
			if err := c.do(ctx, http.MethodPut, "/zones/"+zone+"/dns_records/"+e.ID, recordPayload(r), nil); err != nil {
				return res, err
			}
			res.Updated++
			continue
		}
		if foreign[k] {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("%s record %s already exists and was not created by the panel, left untouched", r.Type, r.Name))
			continue
		}
		if err := c.do(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", recordPayload(r), nil); err != nil {
			return res, err
		}
		res.Created++
	}
	return res, nil
}
