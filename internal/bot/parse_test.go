package bot

import "testing"

func TestParseDetailsLabelledEnglish(t *testing.T) {
	raw := `Enter your full name: Amit Sharma
Describe your complaint: Garbage not collected in our area since 3 days
Enter the address or share the location where this complaint occurred: Near Hanuman Mandir, Jintur`

	got, ok := parseDetails(raw, complaintFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "Amit Sharma" {
		t.Errorf("name = %q", got["name"])
	}
	if got["description"] != "Garbage not collected in our area since 3 days" {
		t.Errorf("description = %q", got["description"])
	}
	if got["address"] != "Near Hanuman Mandir, Jintur" {
		t.Errorf("address = %q", got["address"])
	}
}

func TestParseDetailsLabelledMarathi(t *testing.T) {
	raw := `आपले पूर्ण नाव टाका: अमित शर्मा
आपल्या तक्रारीचे वर्णन करा: आमच्या भागात गेले 3 दिवस कचरा उचलला गेला नाही
जिथे ही तक्रार घडली आहे त्या ठिकाणचा पत्ता टाका किंवा लोकेशन शेयर करा: हनुमान मंदिराजवळ, जिंतूर`

	got, ok := parseDetails(raw, complaintFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "अमित शर्मा" {
		t.Errorf("name = %q", got["name"])
	}
	if got["address"] != "हनुमान मंदिराजवळ, जिंतूर" {
		t.Errorf("address = %q", got["address"])
	}
}

func TestParseDetailsPositional(t *testing.T) {
	raw := "Amit Sharma\nGarbage not collected since 3 days\nNear Hanuman Mandir, Jintur and temple road"

	got, ok := parseDetails(raw, complaintFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "Amit Sharma" {
		t.Errorf("name = %q", got["name"])
	}
	if got["description"] != "Garbage not collected since 3 days" {
		t.Errorf("description = %q", got["description"])
	}
	if got["address"] != "Near Hanuman Mandir, Jintur and temple road" {
		t.Errorf("address = %q", got["address"])
	}
}

func TestParseDetailsIncomplete(t *testing.T) {
	if _, ok := parseDetails("Name: Amit", complaintFields); ok {
		t.Fatal("expected failure for incomplete labelled input")
	}
	if _, ok := parseDetails("only one line", complaintFields); ok {
		t.Fatal("expected failure for incomplete positional input")
	}
}

func TestParseMeetingDetails(t *testing.T) {
	raw := `Full Name:
Preferred Date (DD-MM-YYYY):
Reason for Appointment: Medical help`
	// Empty values must be rejected.
	if _, ok := parseDetails(raw, meetingFields); ok {
		t.Fatal("expected failure for empty values")
	}

	raw = `Full Name: Amit Sharma
Preferred Date (DD-MM-YYYY): 25-12-2026
Reason for Appointment: Discuss road work`
	got, ok := parseDetails(raw, meetingFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "Amit Sharma" {
		t.Errorf("name = %q", got["name"])
	}
	if got["date"] != "25-12-2026" {
		t.Errorf("date = %q", got["date"])
	}
	if got["reason"] != "Discuss road work" {
		t.Errorf("reason = %q", got["reason"])
	}
}

func TestNormalizeDate(t *testing.T) {
	cases := map[string]string{
		"25-12-2026": "2026-12-25",
		"25/12/2026": "2026-12-25",
		"2026-12-25": "2026-12-25",
		"२५-१२-२०२६": "2026-12-25",
	}
	for in, want := range cases {
		got, ok := normalizeDate(in)
		if !ok || got != want {
			t.Errorf("normalizeDate(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := normalizeDate("not-a-date"); ok {
		t.Error("expected failure for invalid date")
	}
}

func TestNormalizeTicket(t *testing.T) {
	cases := map[string]string{
		"TKT4821907":    "TKT4821907",
		"tkt4821907":    "TKT4821907",
		" tkt 4821907 ": "TKT4821907",
		"TKT-4821907":   "TKT4821907",
		"4821907":       "TKT4821907",
		"":              "",
	}
	for in, want := range cases {
		if got := normalizeTicket(in); got != want {
			t.Errorf("normalizeTicket(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestNormalizePin(t *testing.T) {
	if got := normalizePin("४००६०१"); got != "400601" {
		t.Errorf("normalizePin = %q", got)
	}
	if got := asciiToDevanagariDigits("400601"); got != "४००६०१" {
		t.Errorf("asciiToDevanagariDigits = %q", got)
	}
}

func TestMenuRowsCount(t *testing.T) {
	rows := menuRows(I18n["en"])
	if len(rows) != 13 {
		t.Fatalf("menu rows = %d, want 13", len(rows))
	}
	for i, r := range rows {
		want := "opt_" + itoa(i+1)
		if r[0] != want {
			t.Errorf("row %d id = %q, want %q", i, r[0], want)
		}
	}
}

// Meta rejects list messages with more than 10 rows TOTAL (error #131009),
// so any row set (e.g. a long leader list) must be split into chunks of ≤10.
func TestChunkRows(t *testing.T) {
	rows := menuRows(I18n["en"])
	chunks := chunkRows(rows)

	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != 10 {
		t.Errorf("chunk 0 rows = %d, want 10", len(chunks[0]))
	}
	if len(chunks[1]) != 3 {
		t.Errorf("chunk 1 rows = %d, want 3", len(chunks[1]))
	}

	// Order and content must be preserved across chunks.
	var merged [][3]string
	for _, c := range chunks {
		merged = append(merged, c...)
	}
	if len(merged) != len(rows) {
		t.Fatalf("merged rows = %d, want %d", len(merged), len(rows))
	}
	for i, r := range merged {
		if r[0] != rows[i][0] {
			t.Errorf("merged row %d id = %q, want %q", i, r[0], rows[i][0])
		}
	}

	// Exactly 10 rows → single chunk; empty → no chunks.
	if got := chunkRows(rows[:10]); len(got) != 1 {
		t.Errorf("10 rows produced %d chunks, want 1", len(got))
	}
	if got := chunkRows(nil); len(got) != 0 {
		t.Errorf("nil rows produced %d chunks, want 0", len(got))
	}
}

// The main menu shows ONE list (Meta: max 10 rows total): options 1–9
// plus a "More Options" row; tapping it reveals options 10–13.
func TestPrimaryAndSecondaryMenuRows(t *testing.T) {
	for lang := range I18n {
		s := I18n[lang]

		primary := primaryMenuRows(s)
		if len(primary) != metaListMaxRows {
			t.Errorf("[%s] primary rows = %d, want %d", lang, len(primary), metaListMaxRows)
		}
		for i := 0; i < 9; i++ {
			if want := "opt_" + itoa(i+1); primary[i][0] != want {
				t.Errorf("[%s] primary row %d id = %q, want %q", lang, i, primary[i][0], want)
			}
		}
		if primary[9][0] != "opt_more" {
			t.Errorf("[%s] last primary row id = %q, want opt_more", lang, primary[9][0])
		}
		if primary[9][1] != s.OptMoreOptions {
			t.Errorf("[%s] more-row title = %q, want %q", lang, primary[9][1], s.OptMoreOptions)
		}

		secondary := secondaryMenuRows(s)
		if len(secondary) != 4 {
			t.Errorf("[%s] secondary rows = %d, want 4", lang, len(secondary))
		}
		wantIDs := []string{"opt_10", "opt_11", "opt_12", "opt_13"}
		for i, r := range secondary {
			if r[0] != wantIDs[i] {
				t.Errorf("[%s] secondary row %d id = %q, want %q", lang, i, r[0], wantIDs[i])
			}
		}

		// Every menu option must be reachable exactly once.
		seen := map[string]bool{}
		for _, r := range primary {
			seen[r[0]] = true
		}
		for _, r := range secondary {
			seen[r[0]] = true
		}
		for _, r := range menuRows(s) {
			if !seen[r[0]] {
				t.Errorf("[%s] option %q is unreachable", lang, r[0])
			}
		}
	}
}

func itoa(n int) string {
	if n == 10 {
		return "10"
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
