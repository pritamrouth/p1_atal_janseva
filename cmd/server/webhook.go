package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/ataljanseva/janseva-chatbot/internal/whatsapp"
	"github.com/ataljanseva/janseva-chatbot/internal/worker"
)

// webhookHandler handles Meta webhook verification (GET) and inbound events (POST).
type webhookHandler struct {
	verifyToken string
	pool        *worker.Pool
}

func (wh *webhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		wh.verify(w, r)
	case http.MethodPost:
		wh.receive(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /webhook – Meta hub.challenge verification
func (wh *webhookHandler) verify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mode := q.Get("hub.mode")
	token := q.Get("hub.verify_token")
	challenge := q.Get("hub.challenge")

	if mode == "subscribe" && token == wh.verifyToken {
		slog.Info("webhook hub verification OK")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(challenge))
		return
	}
	slog.Warn("webhook hub verification FAILED", "mode", mode)
	http.Error(w, "Forbidden", http.StatusForbidden)
}

// POST /webhook – Meta inbound messages.
// Meta requires a fast 200 ACK, so we respond before parsing.
func (wh *webhookHandler) receive(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("webhook read body", "err", err)
		return
	}

	var probe struct {
		Object string `json:"object"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Object != "whatsapp_business_account" {
		return
	}

	payload, err := whatsapp.ParseMeta(body)
	if err != nil {
		slog.Error("webhook parse", "err", err)
		return
	}

	for _, msg := range payload.Messages() {
		slog.Info("meta message received", "from", msg.From, "type", msg.Type)
		wh.pool.Enqueue(msg)
	}
}
