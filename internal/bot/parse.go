package bot

import (
	"strings"
	"time"
	"unicode"
)

// ─────────────────────────────────────────────
// Detail parsing (complaint / meeting free-text input)
// ─────────────────────────────────────────────

// fieldDef describes one field the parser should extract from a free-text
// message, together with the labels a user may write before the value.
type fieldDef struct {
	key    string
	labels []string
}

var complaintFields = []fieldDef{
	{
		key: "name",
		labels: []string{
			"enter your full name", "your full name", "full name", "your name",
			"your good name", "fullname", "name",
			"आपले पूर्ण नाव टाका", "तुमचे पूर्ण नाव", "आपले पूर्ण नाव", "पूर्ण नाव",
			"आपले नाव", "तुमचे नाव", "माझे नाव", "नाव",
			"अपना पूरा नाम दर्ज करें", "अपना पूरा नाम", "पूरा नाम", "मेरा नाम", "अपना नाम", "नाम",
		},
	},
	{
		key: "description",
		labels: []string{
			"describe your complaint", "complaint description", "complaint details",
			"your complaint", "description", "complaint", "issue", "problem",
			"आपल्या तक्रारीचे वर्णन करा", "तक्रारीचे वर्णन", "तक्रारीचा तपशील", "तक्रार", "समस्या",
			"अपनी शिकायत का वर्णन करें", "शिकायत का विवरण", "शिकायत का वर्णन", "शिकायत", "विवरण", "समस्या",
		},
	},
	{
		key: "address",
		labels: []string{
			"enter the address or share the location where this complaint occurred",
			"address or location", "address / location", "complaint location",
			"where this complaint occurred", "location", "address", "place",
			"जिथे ही तक्रार घडली आहे त्या ठिकाणचा पत्ता टाका किंवा लोकेशन शेयर करा",
			"पत्ता किंवा लोकेशन", "तक्रारीचे ठिकाण", "ठिकाणचा पत्ता", "पत्ता", "लोकेशन", "ठिकाण",
			"वह पता दर्ज करें या लोकेशन शेयर करें जहाँ यह शिकायत हुई है",
			"पता या लोकेशन", "शिकायत का स्थान", "पता", "स्थान", "लोकेशन",
		},
	},
}

var meetingFields = []fieldDef{
	complaintFields[0], // name – same labels
	{
		key: "date",
		labels: []string{
			"preferred date", "appointment date", "meeting date", "date",
			"पसंतीची तारीख", "भेटीची तारीख", "तारीख", "दिनांक",
			"पसंदीदा तारीख", "अपॉइंटमेंट की तारीख", "मीटिंग की तारीख",
		},
	},
	{
		key: "reason",
		labels: []string{
			"reason for appointment", "appointment reason", "reason for meeting",
			"purpose", "reason", "subject",
			"भेटीचे कारण", "भेटीचा उद्देश", "कारण", "विषय",
			"अपॉइंटमेंट का कारण", "मीटिंग का कारण", "कारण", "विषय",
		},
	},
}

// labelSeparators split a "Label: value" line. Natural-language "is" forms
// ("Full name is Amit Sharma") are handled separately in splitLabel.
var labelSeparators = []string{":", "=", "|", "–", "—", "-"}

// noiseLines are template/example headers, not field labels. Skipping them
// lets a citizen paste the whole prompt back with the values filled in.
var noiseLines = map[string]bool{
	"example": true, "for example": true, "eg": true, "e g": true, "e.g": true,
	"format": true, "input format": true, "template": true,
	"उदाहरण": true, "उदाहरणार्थ": true, "स्वरूप": true, "फॉरमॅट": true,
	"उदाहरण के लिए": true, "प्रारूप": true, "फॉर्मेट": true,
}

var noisePrefixes = []string{
	"to raise your complaint", "you can raise your complaint",
	"to schedule a meeting", "to track your complaint",
	"आपण टाइप करून", "आपली तक्रार नोंदवण्यासाठी", "भेट निश्चित करण्यासाठी", "आपल्या तक्रारीचा मागोवा",
	"आप टाइप करके", "अपनी शिकायत दर्ज करने", "मीटिंग तय करने", "अपनी शिकायत को ट्रैक",
}

// parseDetails extracts labelled fields from a multi-line message.
//
// Supported input styles:
//  1. Labelled   – "Enter your full name: Amit Sharma" (any language labels,
//     ":" "=" "|" "-" "–" "—" separators, optional WhatsApp bold markers)
//  2. Natural    – "Full name is Amit Sharma"
//  3. Positional – one value per line, in the order of defs
//
// Extra blank lines, list numbers ("1."), bullets and the template's own
// "Example:" header are ignored. Returns the extracted values (keyed by
// fieldDef.key) and true on success.
func parseDetails(raw string, defs []fieldDef) (map[string]string, bool) {
	out := make(map[string]string, len(defs))
	current := ""
	labeled := false
	var positional []string

	for _, line := range strings.Split(raw, "\n") {
		clean := cleanLine(line)
		if clean == "" || isNoiseLine(clean) {
			continue
		}

		if key, val, ok := splitLabel(clean, defs); ok {
			if field := matchLabel(key, defs); field != "" {
				labeled = true
				current = field
				if v := cleanValue(val); v != "" {
					if out[field] == "" {
						out[field] = v
					} else {
						out[field] += " " + v
					}
				}
				continue
			}
		}

		// A bare label line ("Full Name") — the citizen removed the colon.
		if field := matchExactLabel(clean, defs); field != "" {
			labeled = true
			current = field
			continue
		}

		if current != "" {
			// Continuation of the previously matched field.
			if v := cleanValue(clean); v != "" {
				if out[current] == "" {
					out[current] = v
				} else {
					out[current] += " " + v
				}
			}
			continue
		}

		positional = append(positional, clean)
	}

	if labeled {
		for _, d := range defs {
			out[d.key] = strings.TrimSpace(out[d.key])
			if out[d.key] == "" {
				return nil, false
			}
		}
		return out, true
	}

	// Positional fallback: one value per line in field order.
	if len(positional) < len(defs) {
		return nil, false
	}
	for i, d := range defs {
		if i < len(defs)-1 {
			out[d.key] = cleanValue(positional[i])
		} else {
			out[d.key] = cleanValue(strings.Join(positional[i:], " "))
		}
	}
	return out, true
}

// splitLabel splits a labelled line into its key and value. A separator is
// accepted only when the text before it is one of the known field labels,
// so ordinary values that contain dashes (dates, addresses) are safe.
func splitLabel(line string, defs []fieldDef) (key, val string, ok bool) {
	for _, sep := range labelSeparators {
		i := strings.Index(line, sep)
		if i <= 0 {
			continue
		}
		key = strings.TrimSpace(line[:i])
		if matchLabel(key, defs) == "" {
			continue
		}
		return key, cleanValue(line[i+len(sep):]), true
	}

	// "Full name is Amit Sharma" / "नाव आहे अमित" / "नाम है अमित".
	lower := strings.ToLower(line)
	for _, phrase := range []string{" is ", " आहे ", " है "} {
		if i := strings.Index(lower, phrase); i > 0 {
			key = strings.TrimSpace(line[:i])
			if matchLabel(key, defs) != "" {
				return key, cleanValue(line[i+len(phrase):]), true
			}
		}
	}
	return "", "", false
}

// cleanValue trims whitespace and WhatsApp markdown markers from a value, so
// "*Amit Sharma*" → "Amit Sharma" and a lone "*" is treated as empty.
func cleanValue(s string) string {
	return strings.Trim(strings.TrimSpace(s), "*_~`")
}

// matchExactLabel matches a label after normalisation only (no fuzzy match).
func matchExactLabel(key string, defs []fieldDef) string {
	norm := normalizeLabel(key)
	if norm == "" {
		return ""
	}
	for _, d := range defs {
		for _, l := range d.labels {
			if normalizeLabel(l) == norm {
				return d.key
			}
		}
	}
	return ""
}

// matchLabel returns the field key whose label set contains the given key.
// Comparison is exact after normalisation (lowercase, markdown/punctuation
// stripped, parentheticals removed, whitespace collapsed). Longer labels also
// match when the citizen adds a few words around them.
func matchLabel(key string, defs []fieldDef) string {
	if field := matchExactLabel(key, defs); field != "" {
		return field
	}
	norm := normalizeLabel(key)
	if norm == "" {
		return ""
	}
	for _, d := range defs {
		for _, l := range d.labels {
			nl := normalizeLabel(l)
			if len([]rune(nl)) >= 6 && strings.Contains(norm, nl) {
				return d.key
			}
		}
	}
	return ""
}

func normalizeLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Strip parentheticals: "Preferred Date (DD-MM-YYYY)" → "preferred date"
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
				continue
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	// Drop markdown/bullets/emoji and punctuation inside/around the label.
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, b.String())
	return strings.Join(strings.Fields(cleaned), " ")
}

// cleanLine trims a line and strips common list prefixes ("-", "•", "1. ", "2) ").
func cleanLine(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimLeft(s, "-–—•*#> \t")

	// Strip a leading list counter such as "1. " or "12) ".
	digits := 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(s) && (s[digits] == '.' || s[digits] == ')') {
		rest := s[digits+1:]
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
			s = strings.TrimSpace(rest)
		}
	}
	return strings.TrimSpace(s)
}

func isNoiseLine(line string) bool {
	norm := normalizeLabel(line)
	if noiseLines[norm] {
		return true
	}
	for _, p := range noisePrefixes {
		if strings.HasPrefix(norm, normalizeLabel(p)) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// normalizePin converts Devanagari digits (०-९) to ASCII (0-9).
func normalizePin(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r >= '०' && r <= '९' {
			b.WriteRune('0' + (r - '०'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dateLayouts accepts the formats citizens actually send. Zero-padded and
// unpadded days/months, two- and four-digit years, and common English
// month-name forms are all recognised.
var dateLayouts = []string{
	"02-01-2006", "2-1-2006", "02/01/2006", "2/1/2006",
	"02.01.2006", "2.1.2006", "02 01 2006", "2 1 2006",
	"2006-01-02", "2006-1-2",
	"02-01-06", "2-1-06", "02/01/06", "2/1/06", "02.01.06", "2.1.06",
	"02 Jan 2006", "2 Jan 2006", "02 January 2006", "2 January 2006",
	"Jan 2 2006", "Jan 02 2006", "January 2 2006", "January 02 2006",
	"02-Jan-2006", "2-Jan-2006", "02-January-2006", "2-January-2006",
}

// normalizeDate accepts numeric dates (DD-MM-YYYY / DD/MM/YYYY / DD.MM.YYYY /
// YYYY-MM-DD), English month names and Devanagari digits, and returns the ISO
// form (YYYY-MM-DD) for PostgreSQL.
func normalizeDate(s string) (string, bool) {
	s = strings.TrimSpace(normalizePin(s))
	s = strings.ReplaceAll(s, ",", " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", false
	}
	for _, candidate := range []string{s, titleCaseWords(s)} {
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, candidate); err == nil {
				return t.Format("2006-01-02"), true
			}
		}
	}
	return "", false
}

// titleCaseWords capitalises the first letter of each word so month names are
// accepted in any case ("25 dec 2026" → "25 Dec 2026").
func titleCaseWords(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		r := []rune(f)
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
			fields[i] = string(r)
		}
	}
	return strings.Join(fields, " ")
}

// normalizeTicket turns user input into a canonical ticket ID: uppercase, no
// spaces/dashes. It understands "Ticket ID: TKT4821907", a bare "TKT4821907",
// "tkt-4821907", Devanagari digits and digits-only input (prefixed with "TKT").
func normalizeTicket(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(normalizePin(raw)))
	if s == "" {
		return ""
	}

	// A TKT-prefixed ID anywhere in the message wins.
	if i := strings.Index(s, "TKT"); i >= 0 {
		var digits strings.Builder
		for _, r := range s[i+3:] {
			switch {
			case r >= '0' && r <= '9':
				digits.WriteRune(r)
			case isTicketSeparator(r):
				continue
			}
			if r < '0' || r > '9' {
				break
			}
		}
		if digits.Len() > 0 {
			return "TKT" + digits.String()
		}
	}

	// Otherwise prefer the last token containing a digit, then the last
	// token: "Ticket ID: 4821907" / "माझा तिकीट 4821907 आहे" / "4821907".
	token := ""
	digitToken := ""
	for _, f := range strings.FieldsFunc(s, isTicketSeparator) {
		if f == "" {
			continue
		}
		token = f
		if strings.ContainsAny(f, "0123456789") {
			digitToken = f
		}
	}
	if digitToken != "" {
		token = digitToken
	}
	if token == "" {
		return ""
	}
	allDigits := true
	for _, r := range token {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "TKT" + token
	}
	return token
}

func isTicketSeparator(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '-', '_', '.', ':', ',', '/', '\\', '#', '*', '|', '(', ')', '[', ']', '।':
		return true
	}
	return false
}
