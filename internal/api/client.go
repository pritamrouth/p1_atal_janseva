// Package api is a client for the public AtalJanseva REST API that backs the
// chatbot's complaint, tracking, appointment and information flows.
// Every endpoint is public (no Authorization header); the tenant is identified
// by the nagarsevak slug sent as nagarsevakId. The API never receives file
// bytes — images are shared as public URLs (photoUrl).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production AtalJanseva API.
const DefaultBaseURL = "https://api.ataljanseva.in"

// maxBody caps how much of a response we read (4 MiB).
const maxBody = 4 << 20

// Client calls the AtalJanseva REST API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client for baseURL. An empty baseURL means production;
// a non-positive timeout falls back to 10 seconds.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

// envelope is the single response wrapper used by every endpoint:
// { "success": bool, "message": string, "data": ... }.
type envelope struct {
	Success *bool           `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Error is a non-success API response (HTTP status or success=false).
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("ataljanseva api: HTTP %d: %s", e.Status, e.Message)
}

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// IsNotFound reports whether err is a 404 (unknown ticket ID, empty mobile, …).
func IsNotFound(err error) bool {
	e := asError(err)
	return e != nil && e.Status == http.StatusNotFound
}

// IsBadRequest reports whether err is a 400 (missing/malformed field).
func IsBadRequest(err error) bool {
	e := asError(err)
	return e != nil && e.Status == http.StatusBadRequest
}

// IsSlotTaken reports whether err is the appointment conflict response
// ("This time slot is already booked…").
func IsSlotTaken(err error) bool {
	e := asError(err)
	return e != nil && e.Status == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(e.Message), "already booked")
}

// do performs one request and decodes the envelope's data into out (which may
// be nil, or a *json.RawMessage for shape-agnostic decoding).
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("ataljanseva api: marshal request: %w", err)
		}
		payload = bytes.NewReader(b)
	}

	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, payload)
	if err != nil {
		return fmt.Errorf("ataljanseva api: new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ataljanseva api: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("ataljanseva api: %s %s: read: %w", method, path, err)
	}

	var env envelope
	_ = json.Unmarshal(raw, &env) // tolerate non-JSON bodies

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Message: apiMessage(env, raw, resp.StatusCode)}
	}
	if env.Success != nil && !*env.Success {
		return &Error{Status: resp.StatusCode, Message: apiMessage(env, raw, resp.StatusCode)}
	}
	if out == nil {
		return nil
	}

	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err == nil {
			return nil
		}
	}
	// Fallback: the body itself (an endpoint that skips the envelope).
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("ataljanseva api: %s %s: decode: %w", method, path, err)
	}
	return nil
}

func apiMessage(env envelope, raw []byte, status int) string {
	if strings.TrimSpace(env.Message) != "" {
		return env.Message
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		if msg, ok := m["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	return http.StatusText(status)
}

// ─────────────────────────────────────────────
// Grievances
// ─────────────────────────────────────────────

// GrievanceRequest is the POST /api/grievances body.
// Category defaults to "Other" server-side; source stays unset ("online").
type GrievanceRequest struct {
	NagarsevakID string `json:"nagarsevakId"`
	Name         string `json:"name"`
	Mobile       string `json:"mobile,omitempty"`
	Category     string `json:"category,omitempty"`
	Description  string `json:"description"`
	Address      string `json:"address,omitempty"`
	Ward         string `json:"ward,omitempty"`
}

// Grievance is a grievance as returned by the API.
type Grievance struct {
	ID          string `json:"id"`
	TicketID    string `json:"ticketId"`
	Status      string `json:"status"` // Pending | In Progress | Resolved
	Category    string `json:"category"`
	Ward        string `json:"ward"`
	AssignedTo  string `json:"assignedTo"`
	Description string `json:"description"`
}

// CreateGrievance submits a grievance and returns the created record
// (HTTP 201) carrying the ticketId to send back to the citizen.
func (c *Client) CreateGrievance(ctx context.Context, in GrievanceRequest) (*Grievance, error) {
	var g Grievance
	if err := c.do(ctx, http.MethodPost, "/api/grievances", nil, in, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// TrackTicket fetches one grievance by ticket ID (case-insensitive server-side).
// Unknown IDs return a 404 Error (see IsNotFound).
func (c *Client) TrackTicket(ctx context.Context, ticketID string) (*Grievance, error) {
	var g Grievance
	path := "/api/grievances/track/ticket/" + url.PathEscape(ticketID)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// ─────────────────────────────────────────────
// Appointments
// ─────────────────────────────────────────────

// BookedSlots returns the booked start times for one day as a set of "HH:MM".
// The API's time field may carry seconds ("10:00:00"), so only the first five
// characters are kept. A call failure returns a non-nil error — the caller
// decides whether to offer the slots unfiltered.
func (c *Client) BookedSlots(ctx context.Context, nagarsevakID, date string) (map[string]bool, error) {
	q := url.Values{"nagarsevakId": {nagarsevakID}, "date": {date}}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/schedule/appointments/slots", q, nil, &raw); err != nil {
		return nil, err
	}
	return parseSlotTimes(raw), nil
}

// parseSlotTimes defensively extracts HH:MM start times from the slots payload.
func parseSlotTimes(raw json.RawMessage) map[string]bool {
	set := make(map[string]bool)
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return set
	}
	list, _ := v.([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		for _, key := range []string{"time", "startTime"} {
			if s, ok := m[key].(string); ok && len(s) >= 5 {
				set[s[:5]] = true
				break
			}
		}
	}
	return set
}

// BookRequest is the POST /api/schedule/appointments/book body.
// Date is YYYY-MM-DD (not in the past), time is 24-hour HH:MM; the server
// sets endTime 30 minutes later. A taken slot returns 400 (see IsSlotTaken).
type BookRequest struct {
	NagarsevakID string `json:"nagarsevakId"`
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Date         string `json:"date"`
	Time         string `json:"time"`
	Reason       string `json:"reason,omitempty"`
}

// Appointment is a booked appointment as returned by the API.
type Appointment struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Time    string `json:"time"`
	EndTime string `json:"endTime"`
	Status  string `json:"status"`
}

// BookAppointment books a 30-minute appointment slot.
func (c *Client) BookAppointment(ctx context.Context, in BookRequest) (*Appointment, error) {
	var a Appointment
	if err := c.do(ctx, http.MethodPost, "/api/schedule/appointments/book", nil, in, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// ─────────────────────────────────────────────
// Public listings & profile
// ─────────────────────────────────────────────

// Item is one record from a listing/profile endpoint whose exact schema is not
// fixed. Access fields with FirstStr.
type Item map[string]any

// PublicList fetches a public listing endpoint (data may be a bare array or an
// object wrapping the list) and normalises it into items.
func (c *Client) PublicList(ctx context.Context, path string, q url.Values) ([]Item, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, q, nil, &raw); err != nil {
		return nil, err
	}
	return ItemsFrom(raw), nil
}

// ProfileBySlug fetches GET /api/political-users/slug/{slug} and returns the
// profile object (unwrapping a single nested object when needed).
func (c *Client) ProfileBySlug(ctx context.Context, slug string) (Item, error) {
	var raw json.RawMessage
	path := "/api/political-users/slug/" + url.PathEscape(slug)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &raw); err != nil {
		return nil, err
	}
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil, fmt.Errorf("ataljanseva api: profile %q: decode failed", slug)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ataljanseva api: profile %q: unexpected shape", slug)
	}
	return unwrapItem(m, 0), nil
}

// ItemsFrom decodes data that may be a bare array or an object wrapping one.
func ItemsFrom(raw json.RawMessage) []Item {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	list := findArray(v)
	items := make([]Item, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			items = append(items, Item(m))
		}
	}
	return items
}

// preferredListKeys are plausible wrapper keys for the list inside data.
var preferredListKeys = []string{
	"grievances", "events", "programs", "campaigns", "reports", "workReports",
	"services", "items", "list", "results", "rows", "data",
}

func findArray(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, want := range preferredListKeys {
		for key, val := range m {
			if strings.EqualFold(key, want) {
				if list, ok := val.([]any); ok {
					return list
				}
			}
		}
	}
	for _, val := range m {
		if list, ok := val.([]any); ok {
			return list
		}
	}
	return nil
}

// nameKeys mark an object as the profile itself rather than a wrapper.
var nameKeys = []string{"name", "fullName", "displayName", "username", "title"}

// preferredWrapKeys are wrapper keys checked before descending blindly.
var preferredWrapKeys = []string{"user", "profile", "politicalUser", "data", "result"}

// unwrapItem descends up to 3 levels into wrapper objects until an object that
// looks like a profile (it has a name-ish key) is found.
func unwrapItem(m map[string]any, depth int) Item {
	if depth >= 3 || FirstStr(m, nameKeys...) != "" {
		return Item(m)
	}
	for _, want := range preferredWrapKeys {
		for key, val := range m {
			if strings.EqualFold(key, want) {
				if inner, ok := val.(map[string]any); ok {
					return unwrapItem(inner, depth+1)
				}
			}
		}
	}
	for _, val := range m {
		if inner, ok := val.(map[string]any); ok {
			return unwrapItem(inner, depth+1)
		}
	}
	return Item(m)
}

// FirstStr returns the first non-empty string value among keys
// (case-insensitive key match). Numbers are rendered without trailing zeros,
// so a numeric ward (12) still comes back as "12".
func FirstStr(m map[string]any, keys ...string) string {
	for _, want := range keys {
		for k, v := range m {
			if !strings.EqualFold(k, want) {
				continue
			}
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			}
		}
	}
	return ""
}

// Snippet collapses whitespace and truncates s to max runes.
func Snippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// Mobile10 normalises any phone input to the plain 10-digit number the API
// requires: "919876543210" / "+91 98765 43210" / "09876543210" → "9876543210".
func Mobile10(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	switch {
	case len(d) == 12 && strings.HasPrefix(d, "91"):
		return d[2:]
	case len(d) == 11 && strings.HasPrefix(d, "0"):
		return d[1:]
	}
	return d
}
