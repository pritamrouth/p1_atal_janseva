package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/ataljanseva/janseva-chatbot/internal/api"
)

func TestNumbered(t *testing.T) {
	items := []api.Item{
		{"title": "First"},
		{"title": "Second"},
		{"empty": "   "}, // blank body → skipped
	}
	lines := numbered(items, 5, func(it api.Item) string {
		return api.FirstStr(it, "title")
	})
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if !strings.HasPrefix(lines[0], "1. First") {
		t.Errorf("line 0 = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "2. Second") {
		t.Errorf("line 1 = %q", lines[1])
	}

	// Cap respected.
	many := make([]api.Item, 10)
	for i := range many {
		many[i] = api.Item{"title": "x"}
	}
	if got := numbered(many, maxInfoItems, formatInfoItem); len(got) != maxInfoItems {
		t.Errorf("capped lines = %d, want %d", len(got), maxInfoItems)
	}
}

func TestFormatInfoItem(t *testing.T) {
	// Full record: title, meta, description.
	got := formatInfoItem(api.Item{
		"title":       "Diwali Milan",
		"date":        "2026-10-20",
		"time":        "18:00",
		"location":    "Town Hall",
		"description": "Annual gathering",
	})
	want := "Diwali Milan\n2026-10-20 · 18:00 · Town Hall\nAnnual gathering"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Title only.
	if got := formatInfoItem(api.Item{"name": "PM Awas"}); got != "PM Awas" {
		t.Errorf("title only = %q", got)
	}

	// Nothing extractable → placeholder.
	if got := formatInfoItem(api.Item{"id": 7}); got != "—" {
		t.Errorf("empty = %q", got)
	}

	// Description identical to title is not repeated.
	got = formatInfoItem(api.Item{"title": "Same", "description": "Same"})
	if got != "Same" {
		t.Errorf("dup description = %q", got)
	}
}

func TestFormatGrievance(t *testing.T) {
	got := formatGrievance(api.Item{
		"ticketId":       "TKT4821907",
		"category":       "Water",
		"ward":           "12",
		"assignedTo":     "Sunita Jadhav",
		"resolutionNote": "Pipeline repaired",
	})
	if !strings.HasPrefix(got, "TKT4821907 · Water · 12 · Sunita Jadhav") {
		t.Errorf("line = %q", got)
	}
	if !strings.Contains(got, "Pipeline repaired") {
		t.Errorf("missing note: %q", got)
	}

	// Sparse record does not produce stray separators.
	if got := formatGrievance(api.Item{"ticketId": "TKT1"}); got != "TKT1" {
		t.Errorf("sparse = %q", got)
	}
}

func TestFormatProfile(t *testing.T) {
	text, photo := formatProfile(api.Item{
		"name":        "Ashok Shelke",
		"designation": "Corporator",
		"party":       "BJP",
		"bio":         "Serving the ward since 2010",
		"photoUrl":    "https://cdn/x.jpg",
	}, "Fallback")
	if !strings.Contains(text, "Ashok Shelke") ||
		!strings.Contains(text, "Corporator · BJP") ||
		!strings.Contains(text, "Serving the ward since 2010") {
		t.Errorf("profile text = %q", text)
	}
	if photo != "https://cdn/x.jpg" {
		t.Errorf("photo = %q", photo)
	}

	// No name in the profile → fallback used, no stray blank sections.
	text, photo = formatProfile(api.Item{}, "Ashok Shelke")
	if !strings.Contains(text, "Ashok Shelke") {
		t.Errorf("fallback text = %q", text)
	}
	if photo != "" {
		t.Errorf("photo = %q, want empty", photo)
	}
}

func TestListMessage(t *testing.T) {
	got := listMessage("Header:", []string{"1. A", "2. B"}, "footer")
	want := "Header:\n\n1. A\n\n2. B\n\nfooter"
	if got != want {
		t.Errorf("listMessage = %q", got)
	}
}

func TestFreeMeetingSlots(t *testing.T) {
	// Nothing booked → all four candidates.
	all := freeMeetingSlots(nil)
	if len(all) != len(meetingSlots) {
		t.Fatalf("free = %d, want %d", len(all), len(meetingSlots))
	}

	// 10:00 and 17:00 booked → 12:00 and 15:00 remain, order preserved.
	free := freeMeetingSlots(map[string]bool{"10:00": true, "17:00": true})
	if len(free) != 2 {
		t.Fatalf("free = %d, want 2", len(free))
	}
	if free[0][2] != "12:00" || free[1][2] != "15:00" {
		t.Errorf("free times = %q, %q", free[0][2], free[1][2])
	}

	// Everything booked → empty (caller asks for another date).
	if got := freeMeetingSlots(map[string]bool{
		"10:00": true, "12:00": true, "15:00": true, "17:00": true,
	}); len(got) != 0 {
		t.Errorf("all booked → %v", got)
	}
}

func TestSlotByID(t *testing.T) {
	if s := slotByID("slot_1500"); s == nil || s[2] != "15:00" {
		t.Errorf("slotByID = %v", s)
	}
	if s := slotByID("opt_1"); s != nil {
		t.Errorf("unknown id → %v", s)
	}
	// Every candidate slot must be reachable by its row id.
	for _, s := range meetingSlots {
		if slotByID(s[0]) == nil {
			t.Errorf("slot %q not reachable", s[0])
		}
	}
}

func TestIsPastDate(t *testing.T) {
	if isPastDate("2000-01-01") != true {
		t.Error("past date should be rejected")
	}
	if isPastDate("not-a-date") != true {
		t.Error("invalid date should be rejected")
	}
	if isPastDate("2999-12-31") != false {
		t.Error("future date should be accepted")
	}
	if isPastDate(time.Now().Format("2006-01-02")) != false {
		t.Error("today should be accepted (the API allows it)")
	}
}
