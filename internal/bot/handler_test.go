package bot

import (
	"context"
	"testing"

	"janseva_chatbot/internal/store"
	"janseva_chatbot/internal/whatsapp"
)

// recordingMessenger records the text and list messages the handler sends.
type recordingMessenger struct {
	lists    int
	texts    []string
	lastRows [][3]string
}

func (m *recordingMessenger) SendText(_ context.Context, _, text string) error {
	m.texts = append(m.texts, text)
	return nil
}
func (m *recordingMessenger) SendImage(context.Context, string, string, string) error {
	return nil
}
func (m *recordingMessenger) SendButtonsWithImageHeader(context.Context, string, string, string, [][2]string) error {
	return nil
}
func (m *recordingMessenger) SendList(_ context.Context, _, _, _ string, sections []whatsapp.ListSection) error {
	m.lists++
	if len(sections) > 0 {
		m.lastRows = sections[0].Rows
	}
	return nil
}
func (m *recordingMessenger) DownloadMedia(context.Context, string) ([]byte, string, error) {
	return nil, "", nil
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

func TestDispatchInfoOptionsSendSameMessage(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	want := I18n[defaultLang].TransparencyText + "\n\n" + I18n[defaultLang].ThankYou

	for n := 4; n <= 10; n++ {
		m.texts = nil
		sess := &store.Session{Step: store.StepMainMenu, Lang: defaultLang}
		h.dispatchOption(context.Background(), "919000000000", sess, n)

		if len(m.texts) != 1 {
			t.Fatalf("option %d sent %d texts, want 1", n, len(m.texts))
		}
		if m.texts[0] != want {
			t.Errorf("option %d text = %q, want %q", n, m.texts[0], want)
		}
	}
}

func TestMainMenuEndsWithMoreOptions(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	sess := &store.Session{Lang: defaultLang}

	h.sendMenuList(context.Background(), "919000000000", sess)

	if len(m.lastRows) != metaListMaxRows {
		t.Fatalf("main menu rows = %d, want %d", len(m.lastRows), metaListMaxRows)
	}
	last := m.lastRows[len(m.lastRows)-1]
	if last[0] != "opt_more" || last[1] != "अधिक पर्याय" {
		t.Errorf("last row = %#v, want opt_more / अधिक पर्याय", last)
	}
}

func TestMoreOptionsRowSendsRemainingOptions(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	sess := &store.Session{Step: store.StepMainMenu, Lang: defaultLang}

	h.handleMenuSelection(context.Background(), "919000000000", sess, &whatsapp.InteractiveReply{
		ListReply: &whatsapp.ListReplyData{ID: "opt_more"},
	})

	if m.lists != 1 {
		t.Fatalf("more options sent %d lists, want 1", m.lists)
	}
	if len(m.lastRows) != 1 || m.lastRows[0][0] != "opt_10" {
		t.Errorf("more options rows = %#v, want opt_10", m.lastRows)
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
