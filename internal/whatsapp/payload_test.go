package whatsapp

import "testing"

func TestMessagesParsesAudioAndContactName(t *testing.T) {
	raw := []byte(`{
		"object": "whatsapp_business_account",
		"entry": [{
			"changes": [{
				"value": {
					"contacts": [{
						"profile": {"name": "Rahul Patil"},
						"wa_id": "919876543210"
					}],
					"messages": [{
						"from": "919876543210",
						"id": "wamid.audio1",
						"timestamp": "1730000000",
						"type": "audio",
						"audio": {
							"id": "media-123",
							"mime_type": "audio/ogg; codecs=opus",
							"voice": true
						}
					}]
				}
			}]
		}]
	}`)

	p, err := ParseMeta(raw)
	if err != nil {
		t.Fatalf("ParseMeta: %v", err)
	}
	msgs := p.Messages()
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	msg := msgs[0]
	if msg.Type != "audio" || msg.Audio == nil {
		t.Fatalf("audio message = %#v", msg)
	}
	if msg.Audio.ID != "media-123" || !msg.Audio.Voice {
		t.Errorf("audio = %#v", msg.Audio)
	}
	if msg.Audio.MimeType != "audio/ogg; codecs=opus" {
		t.Errorf("mime = %q", msg.Audio.MimeType)
	}
	if msg.ContactName != "Rahul Patil" {
		t.Errorf("contact name = %q", msg.ContactName)
	}
}
