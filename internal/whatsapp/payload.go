package whatsapp

import "encoding/json"

// ─────────────────────────────────────────────
// Inbound webhook payload (Meta Cloud API v20)
// ─────────────────────────────────────────────

// WebhookPayload is the top-level envelope sent to your webhook endpoint.
type WebhookPayload struct {
	Object string  `json:"object"`
	Entry  []Entry `json:"entry"`
}

type Entry struct {
	ID      string   `json:"id"`
	Changes []Change `json:"changes"`
}

type Change struct {
	Value Value  `json:"value"`
	Field string `json:"field"`
}

type Value struct {
	MessagingProduct string    `json:"messaging_product"`
	Metadata         Metadata  `json:"metadata"`
	Contacts         []Contact `json:"contacts"`
	Messages         []Message `json:"messages"`
	Statuses         []Status  `json:"statuses"`
}

type Metadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type Contact struct {
	Profile Profile `json:"profile"`
	WaID    string  `json:"wa_id"`
}

type Profile struct {
	Name string `json:"name"`
}

type Message struct {
	From        string            `json:"from"`
	ID          string            `json:"id"`
	Timestamp   string            `json:"timestamp"`
	Type        string            `json:"type"` // text | interactive | audio | ...
	Text        *TextMessage      `json:"text,omitempty"`
	Interactive *InteractiveReply `json:"interactive,omitempty"`
	Audio       *AudioMessage     `json:"audio,omitempty"`
	Location    *LocationMessage  `json:"location,omitempty"`
	// ContactName is the WhatsApp profile name of the sender, filled in by
	// Messages() from the webhook's contacts array.
	ContactName string `json:"-"`
}

type TextMessage struct {
	Body string `json:"body"`
}

// AudioMessage is an inbound voice note or audio attachment.
type AudioMessage struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
	Voice    bool   `json:"voice"`
}

// LocationMessage is a location shared by a WhatsApp user.
type LocationMessage struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
}

type InteractiveReply struct {
	Type        string           `json:"type"` // "button_reply" | "list_reply"
	ButtonReply *ButtonReplyData `json:"button_reply,omitempty"`
	ListReply   *ListReplyData   `json:"list_reply,omitempty"`
}

type ButtonReplyData struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ListReplyData struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type Status struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
}

// ParseMeta unmarshals a raw JSON body into a Meta WebhookPayload.
func ParseMeta(data []byte) (*WebhookPayload, error) {
	var p WebhookPayload
	return &p, json.Unmarshal(data, &p)
}

// Messages returns the flat list of inbound messages across all entries/changes.
// Each message is enriched with the sender's WhatsApp profile name so the
// complaint flow can use it when a voice note has no typed name.
func (p *WebhookPayload) Messages() []Message {
	var out []Message
	for _, e := range p.Entry {
		for _, c := range e.Changes {
			names := make(map[string]string, len(c.Value.Contacts))
			for _, contact := range c.Value.Contacts {
				if contact.WaID != "" && contact.Profile.Name != "" {
					names[contact.WaID] = contact.Profile.Name
				}
			}
			for _, msg := range c.Value.Messages {
				if msg.ContactName == "" {
					msg.ContactName = names[msg.From]
				}
				out = append(out, msg)
			}
		}
	}
	return out
}
