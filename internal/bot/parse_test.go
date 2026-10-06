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
		"AJ100001":    "AJ100001",
		"aj100001":    "AJ100001",
		" aj 100001 ": "AJ100001",
		"AJ-100001":   "AJ100001",
		"100001":      "AJ100001",
		"":            "",
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

func itoa(n int) string {
	if n == 10 {
		return "10"
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
