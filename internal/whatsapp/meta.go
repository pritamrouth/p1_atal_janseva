// Package whatsapp wraps the Meta WhatsApp Cloud API v20.
// The HTTP client is tuned for high concurrency:
//   - persistent connection pool (100 idle per host)
//   - exponential backoff retry on 5xx / timeout (up to 3 attempts)
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const apiBase = "https://graph.facebook.com/v20.0"

// MetaClient is a high-concurrency WhatsApp Cloud API client.
type MetaClient struct {
	phoneNumberID string
	accessToken   string
	http          *http.Client
}

// NewMetaClient returns a MetaClient with a tuned HTTP transport.
func NewMetaClient(phoneNumberID, accessToken string) *MetaClient {
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     60 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &MetaClient{
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		http: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}
}

// ─────────────────────────────────────────────
// Payload types – text
// ─────────────────────────────────────────────

type textBody struct {
	MessagingProduct string  `json:"messaging_product"`
	To               string  `json:"to"`
	Type             string  `json:"type"`
	Text             textObj `json:"text"`
}
type textObj struct {
	PreviewURL bool   `json:"preview_url"`
	Body       string `json:"body"`
}

// ─────────────────────────────────────────────
// Payload types – image
// ─────────────────────────────────────────────

type imageBody struct {
	MessagingProduct string   `json:"messaging_product"`
	To               string   `json:"to"`
	Type             string   `json:"type"`
	Image            imageObj `json:"image"`
}
type imageObj struct {
	Link    string `json:"link"`
	Caption string `json:"caption,omitempty"`
}

// ─────────────────────────────────────────────
// Payload types – interactive (buttons / list)
// ─────────────────────────────────────────────

type interactiveBody struct {
	MessagingProduct string         `json:"messaging_product"`
	To               string         `json:"to"`
	Type             string         `json:"type"`
	Interactive      interactiveObj `json:"interactive"`
}
type interactiveObj struct {
	Type   string          `json:"type"`
	Header *interactHeader `json:"header,omitempty"`
	Body   interactText    `json:"body"`
	Footer *interactText   `json:"footer,omitempty"`
	Action interactAction  `json:"action"`
}
type interactHeader struct {
	Type  string    `json:"type"`
	Text  string    `json:"text,omitempty"`
	Image *imageObj `json:"image,omitempty"`
}
type interactText struct {
	Text string `json:"text"`
}
type interactAction struct {
	Buttons    []interactButton `json:"buttons,omitempty"`
	ButtonText string           `json:"button,omitempty"`
	Sections   []listSection    `json:"sections,omitempty"`
}
type interactButton struct {
	Type  string      `json:"type"`
	Reply buttonReply `json:"reply"`
}
type buttonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type listSection struct {
	Title string    `json:"title"`
	Rows  []listRow `json:"rows"`
}
type listRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// ─────────────────────────────────────────────
// Public send methods – Messenger interface
// ─────────────────────────────────────────────

func (c *MetaClient) SendText(ctx context.Context, to, text string) error {
	return c.post(ctx, textBody{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "text",
		Text:             textObj{Body: text},
	})
}

func (c *MetaClient) SendImage(ctx context.Context, to, imageURL, caption string) error {
	return c.post(ctx, imageBody{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "image",
		Image: imageObj{
			Link:    imageURL,
			Caption: caption,
		},
	})
}

func (c *MetaClient) SendButtonsWithImageHeader(ctx context.Context, to, bodyText, imageURL string, buttons [][2]string) error {
	btns := make([]interactButton, 0, len(buttons))
	for _, b := range buttons {
		btns = append(btns, interactButton{
			Type:  "reply",
			Reply: buttonReply{ID: b[0], Title: truncate(b[1], 20)},
		})
	}
	msg := interactiveBody{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "interactive",
		Interactive: interactiveObj{
			Type:   "button",
			Body:   interactText{Text: bodyText},
			Action: interactAction{Buttons: btns},
		},
	}
	if imageURL != "" {
		msg.Interactive.Header = &interactHeader{Type: "image", Image: &imageObj{Link: imageURL}}
	}
	return c.post(ctx, msg)
}

func (c *MetaClient) SendList(ctx context.Context, to, bodyText, buttonLabel string, sections []ListSection) error {
	waSections := make([]listSection, 0, len(sections))
	for _, s := range sections {
		rows := make([]listRow, 0, len(s.Rows))
		for _, r := range s.Rows {
			rows = append(rows, listRow{ID: r[0], Title: truncate(r[1], 24), Description: r[2]})
		}
		waSections = append(waSections, listSection{Title: s.Title, Rows: rows})
	}
	return c.post(ctx, interactiveBody{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "interactive",
		Interactive: interactiveObj{
			Type: "list",
			Body: interactText{Text: bodyText},
			Action: interactAction{
				ButtonText: buttonLabel,
				Sections:   waSections,
			},
		},
	})
}

// ─────────────────────────────────────────────
// Internal – post with exponential backoff retry
// ─────────────────────────────────────────────

const maxRetries = 3

var retryBackoff = [maxRetries]time.Duration{
	200 * time.Millisecond,
	600 * time.Millisecond,
	1500 * time.Millisecond,
}

func (c *MetaClient) post(ctx context.Context, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	url := fmt.Sprintf("%s/%s/messages", apiBase, c.phoneNumberID)

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryBackoff[attempt-1]):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.accessToken)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			slog.Warn("WA API request failed, retrying", "attempt", attempt+1, "err", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 500 {
			if resp.StatusCode >= 400 {
				return fmt.Errorf("WA API %d: %s", resp.StatusCode, string(body))
			}
			return nil
		}

		lastErr = fmt.Errorf("attempt %d: WA API %d: %s", attempt+1, resp.StatusCode, string(body))
		slog.Warn("WA API 5xx, retrying", "attempt", attempt+1, "status", resp.StatusCode)
	}
	return fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
