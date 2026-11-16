package bot

import (
	"context"
	"testing"

	"janseva_chatbot/internal/store"
	"janseva_chatbot/internal/whatsapp"
)

// recordingMessenger counts the list messages the handler sends.
type recordingMessenger struct {
	lists int
}

func (m *recordingMessenger) SendText(context.Context, string, string) error { return nil }
func (m *recordingMessenger) SendImage(context.Context, string, string, string) error {
	return nil
}
func (m *recordingMessenger) SendButtonsWithImageHeader(context.Context, string, string, string, [][2]string) error {
	return nil
}
func (m *recordingMessenger) SendList(context.Context, string, string, string, []whatsapp.ListSection) error {
	m.lists++
	return nil
}

func TestHandleMenuSelectionIgnoresStaleSlot(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	sess := &store.Session{Step: store.StepMainMenu, Lang: defaultLang}

	h.handleMenuSelection(context.Background(), "919000000000", sess, &whatsapp.InteractiveReply{
		ListReply: &whatsapp.ListReplyData{ID: "slot_1000"},
	})

	if m.lists != 0 {
		t.Fatalf("stale slot selection sent %d lists, want 0", m.lists)
	}
}

func TestHandleMenuSelectionRetiredOptionRedrawsMenu(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	sess := &store.Session{Step: store.StepMainMenu, Lang: defaultLang}

	// A stale row from the retired 11–13 options must not be dispatched;
	// the handler simply redraws the current 10-option menu.
	h.handleMenuSelection(context.Background(), "919000000000", sess, &whatsapp.InteractiveReply{
		ListReply: &whatsapp.ListReplyData{ID: "opt_11"},
	})

	if m.lists != 1 {
		t.Fatalf("retired option selection sent %d lists, want 1", m.lists)
	}
}
