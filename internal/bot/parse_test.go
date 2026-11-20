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

func TestParseComplaintDetailsWithoutAddress(t *testing.T) {
	raw := `Full Name: Amit Sharma
Describe your complaint: Garbage not collected since 3 days`

	got, ok := parseDetailsRequired(raw, complaintFields, 2)
	if !ok {
		t.Fatal("expected name and description to parse without an address")
	}
	if got["name"] != "Amit Sharma" || got["description"] != "Garbage not collected since 3 days" {
		t.Errorf("details = %#v", got)
	}
	if _, ok := parseDetailsRequired("Full Name: Amit", complaintFields, 2); ok {
		t.Fatal("expected failure when description is missing")
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

func TestParseMeetingDetailsNewPrompt(t *testing.T) {
	// The appointment prompt now shows plain values in its example.
	got, ok := parseDetails("राहुल पाटील\n20-11-2026\nपाणीपुरवठ्याबाबत तक्रार", meetingFields)
	if !ok {
		t.Fatal("expected plain appointment example to parse")
	}
	if got["date"] != "20-11-2026" {
		t.Errorf("date = %q", got["date"])
	}

	// "भेटीची तारीख" is the label used by the new Marathi prompt.
	raw := `पूर्ण नाव: राहुल पाटील
भेटीची तारीख (DD-MM-YYYY): 20-11-2026
भेटीचे कारण: पाणीपुरवठ्याबाबत तक्रार`
	got, ok = parseDetails(raw, meetingFields)
	if !ok {
		t.Fatal("expected labelled appointment details to parse")
	}
	if got["name"] != "राहुल पाटील" || got["date"] != "20-11-2026" ||
		got["reason"] != "पाणीपुरवठ्याबाबत तक्रार" {
		t.Errorf("meeting details = %#v", got)
	}
}

func TestNormalizeDate(t *testing.T) {
	cases := map[string]string{
		"25-12-2026":       "2026-12-25",
		"25/12/2026":       "2026-12-25",
		"25.12.2026":       "2026-12-25",
		"2026-12-25":       "2026-12-25",
		"२५-१२-२०२६":       "2026-12-25",
		"25 Dec 2026":      "2026-12-25",
		"25 december 2026": "2026-12-25",
		"Dec 25, 2026":     "2026-12-25",
		"25-12-26":         "2026-12-25",
		"5-1-2026":         "2026-01-05",
	}
	for in, want := range cases {
		got, ok := normalizeDate(in)
		if !ok || got != want {
			t.Errorf("normalizeDate(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"not-a-date", "31-02-2026", ""} {
		if _, ok := normalizeDate(in); ok {
			t.Errorf("normalizeDate(%q) unexpectedly succeeded", in)
		}
	}
}

func TestNormalizeTicket(t *testing.T) {
	cases := map[string]string{
		"TKT4821907":            "TKT4821907",
		"tkt4821907":            "TKT4821907",
		" tkt 4821907 ":         "TKT4821907",
		"TKT-4821907":           "TKT4821907",
		"4821907":               "TKT4821907",
		"Ticket ID: TKT4821907": "TKT4821907",
		"ticket id - 4821907":   "TKT4821907",
		"*TKT4821907*":          "TKT4821907",
		"टिकीट ID: TKT४८२१९०७":   "TKT4821907",
		"माझा तिकीट ४८२१९०७ आहे": "TKT4821907",
		"": "",
	}
	for in, want := range cases {
		if got := normalizeTicket(in); got != want {
			t.Errorf("normalizeTicket(%q) = %q; want %q", in, got, want)
		}
	}
}

// The prompts ask for blank lines between fields, but citizens may paste the
// whole template, use dashes, bold labels or natural "is" phrasing.
func TestParseDetailsRobustFormats(t *testing.T) {
	raw := `To raise your complaint, please send your details in this format:

*Enter your full name:*

*Amit Sharma*

Describe your complaint - Garbage not collected in our area since 3 days

Address or location = Near Hanuman Mandir, Jintur`

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

func TestParseDetailsNaturalLanguage(t *testing.T) {
	raw := `Name is Amit Sharma
My complaint is garbage not collected since 3 days
Address is Near Hanuman Mandir, Jintur`

	got, ok := parseDetails(raw, complaintFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "Amit Sharma" {
		t.Errorf("name = %q", got["name"])
	}
	if got["description"] != "garbage not collected since 3 days" {
		t.Errorf("description = %q", got["description"])
	}
	if got["address"] != "Near Hanuman Mandir, Jintur" {
		t.Errorf("address = %q", got["address"])
	}
}

func TestParseDetailsBareLabels(t *testing.T) {
	raw := `Full Name
Amit Sharma

Describe your complaint
Garbage not collected in our area since 3 days

Address or location
Near Hanuman Mandir, Jintur`

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

// The prompt now shows a plain-value example (no field labels), so a citizen
// may simply send three spaced values in order, even after pasting the intro.
func TestParseDetailsPlainExampleFormat(t *testing.T) {
	raw := `आपण ⌨️ टाइप करून किंवा 🎤 व्हॉइस मेसेजद्वारे आपली तक्रार नोंदवू शकता.
आपली तक्रार नोंदवण्यासाठी, कृपया आपली माहिती या फॉरमॅटमध्ये पाठवा:
अमित शर्मा

आमच्या भागात गेले 3 दिवस कचरा उचलला गेला नाही

हनुमान चौक, विठ्ठल रुक्मिणी मंदिराजवळ, जिंतूर`

	got, ok := parseDetails(raw, complaintFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "अमित शर्मा" {
		t.Errorf("name = %q", got["name"])
	}
	if got["description"] != "आमच्या भागात गेले 3 दिवस कचरा उचलला गेला नाही" {
		t.Errorf("description = %q", got["description"])
	}
	if got["address"] != "हनुमान चौक, विठ्ठल रुक्मिणी मंदिराजवळ, जिंतूर" {
		t.Errorf("address = %q", got["address"])
	}
}

func TestParseDetailsIgnoresExampleHeader(t *testing.T) {
	raw := `Enter your full name:
Amit Sharma

Example:
Describe your complaint:
Garbage not collected in our area since 3 days

Enter the address or share the location where this complaint occurred:
Near Hanuman Mandir, Jintur`

	got, ok := parseDetails(raw, complaintFields)
	if !ok {
		t.Fatal("expected parse success")
	}
	if got["name"] != "Amit Sharma" || got["address"] != "Near Hanuman Mandir, Jintur" {
		t.Errorf("parsed = %#v", got)
	}
}

func TestNormalizePin(t *testing.T) {
	if got := normalizePin("४००६०१"); got != "400601" {
		t.Errorf("normalizePin = %q", got)
	}
}

// The main menu shows ONE list with exactly 10 rows (Meta's hard limit):
// options 1–9 plus NariShakti as option 10. Options 11–13 were retired.
func TestMenuRows(t *testing.T) {
	for lang := range I18n {
		s := I18n[lang]
		rows := menuRows(s)
		if len(rows) != 10 {
			t.Errorf("[%s] menu rows = %d, want 10", lang, len(rows))
			continue
		}
		for i, r := range rows {
			want := "opt_" + itoa(i+1)
			if r[0] != want {
				t.Errorf("[%s] row %d id = %q, want %q", lang, i, r[0], want)
			}
		}
		if rows[9][0] != "opt_10" || rows[9][1] != s.OptNariShakti {
			t.Errorf("[%s] last row = %q/%q, want NariShakti", lang, rows[9][0], rows[9][1])
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
