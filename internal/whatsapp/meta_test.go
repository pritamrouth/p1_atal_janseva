package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient returns a MetaClient pointed at a local test server and a
// pointer to the decoded request bodies it receives.
func newTestClient(t *testing.T, messagingAccountID string) (*MetaClient, *[]map[string]any) {
	t.Helper()

	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/phone-id/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.test"}]}`))
	}))
	t.Cleanup(srv.Close)

	c := NewMetaClient("phone-id", "test-token", messagingAccountID)
	c.baseURL = srv.URL
	return c, &bodies
}

func TestSendMethodsIncludeMessagingAccountID(t *testing.T) {
	const accountID = "2029916228410428"

	c, bodies := newTestClient(t, accountID)
	ctx := context.Background()

	senders := []struct {
		name string
		send func() error
	}{
		{"text", func() error {
			return c.SendText(ctx, "919876543210", "hello")
		}},
		{"image", func() error {
			return c.SendImage(ctx, "919876543210", "https://example.com/banner.png", "hello")
		}},
		{"buttons", func() error {
			return c.SendButtonsWithImageHeader(ctx, "919876543210", "hello", "https://example.com/banner.png",
				[][2]string{{"lang_en", "English"}})
		}},
		{"list", func() error {
			return c.SendList(ctx, "919876543210", "hello", "Choose", []ListSection{
				{Title: "Options", Rows: [][3]string{{"opt_1", "First", ""}}},
			})
		}},
	}

	for _, s := range senders {
		t.Run(s.name, func(t *testing.T) {
			before := len(*bodies)
			if err := s.send(); err != nil {
				t.Fatalf("send: %v", err)
			}
			got := *bodies
			if len(got) != before+1 {
				t.Fatalf("requests = %d, want %d", len(got), before+1)
			}
			if got[len(got)-1]["messaging_account_id"] != accountID {
				t.Errorf("messaging_account_id = %v, want %q",
					got[len(got)-1]["messaging_account_id"], accountID)
			}
		})
	}
}

func TestSendTextOmitsMessagingAccountIDWhenUnset(t *testing.T) {
	c, bodies := newTestClient(t, "")

	if err := c.SendText(context.Background(), "919876543210", "hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	got := *bodies
	if len(got) != 1 {
		t.Fatalf("requests = %d, want 1", len(got))
	}
	if _, ok := got[0]["messaging_account_id"]; ok {
		t.Errorf("messaging_account_id should be omitted, body = %v", got[0])
	}
}
