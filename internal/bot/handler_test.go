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
	want := "नेत्याबद्दल माहिती, पारदर्शकता फलक, कामाचा अहवाल, कार्यक्रम आणि बैठकीची माहिती, योजना आणि सेवा, जनसेवा कनेक्ट, नारीशक्ती, आणि अधिक पर्याय पाहण्यासाठी, कृपया भेट द्या: ataljanseva.in\n\n" +
		"अटलजनसेवा नागरिक सेवेशी जोडलेले राहिल्याबद्दल धन्यवाद !!"

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

func TestMainMenuIncludesNariShakti(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	sess := &store.Session{Lang: defaultLang}

	h.sendMenuList(context.Background(), "919000000000", sess)

	if len(m.lastRows) != metaListMaxRows {
		t.Fatalf("main menu rows = %d, want %d", len(m.lastRows), metaListMaxRows)
	}
	last := m.lastRows[len(m.lastRows)-1]
	if last[0] != "opt_10" || last[1] != I18n[defaultLang].OptNariShakti {
		t.Errorf("last row = %#v, want opt_10 / %q", last, I18n[defaultLang].OptNariShakti)
	}
}

func TestLegacyMoreOptionsSelectionRedrawsFullMenu(t *testing.T) {
	m := &recordingMessenger{}
	h := &Handler{wa: m}
	sess := &store.Session{Step: store.StepMainMenu, Lang: defaultLang}

	h.handleMenuSelection(context.Background(), "919000000000", sess, &whatsapp.InteractiveReply{
		ListReply: &whatsapp.ListReplyData{ID: "opt_more"},
	})

	if m.lists != 1 {
		t.Fatalf("legacy more-options selection sent %d lists, want 1", m.lists)
	}
	if len(m.lastRows) != metaListMaxRows {
		t.Fatalf("legacy more-options selection returned %d rows, want %d", len(m.lastRows), metaListMaxRows)
	}
	last := m.lastRows[len(m.lastRows)-1]
	if last[0] != "opt_10" || last[1] != I18n[defaultLang].OptNariShakti {
		t.Errorf("last row = %#v, want opt_10 / %q", last, I18n[defaultLang].OptNariShakti)
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

func TestComplaintLocationAddress(t *testing.T) {
	tests := []struct {
		name     string
		location *whatsapp.LocationMessage
		want     string
		wantErr  bool
	}{
		{
			name: "address and place name",
			location: &whatsapp.LocationMessage{
				Name:    "Town Hall",
				Address: "Main Road, Jintur",
			},
			want: "Town Hall, Main Road, Jintur",
		},
		{
			name: "coordinates fallback",
			location: &whatsapp.LocationMessage{
				Latitude:  19.8342,
				Longitude: 76.1094,
			},
			want: "Coordinates: 19.834200, 76.109400 (https://maps.google.com/?q=19.834200,76.109400)",
		},
		{
			name:     "missing location data",
			location: &whatsapp.LocationMessage{},
			wantErr:  true,
		},
		{
			name: "out of range coordinates",
			location: &whatsapp.LocationMessage{
				Latitude:  91,
				Longitude: 76,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := complaintLocationAddress(tt.location)
			if (err != nil) != tt.wantErr {
				t.Fatalf("complaintLocationAddress() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("complaintLocationAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
