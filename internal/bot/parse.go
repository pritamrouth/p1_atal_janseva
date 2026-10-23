package bot

import (
	"strings"
	"time"
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
			"enter your full name", "full name", "your name", "name",
			"आपले पूर्ण नाव टाका", "पूर्ण नाव", "आपले नाव", "नाव",
			"अपना पूरा नाम दर्ज करें", "पूरा नाम", "अपना नाम", "नाम",
		},
	},
	{
		key: "description",
		labels: []string{
			"describe your complaint", "complaint description", "your complaint",
			"description", "complaint",
			"आपल्या तक्रारीचे वर्णन करा", "तक्रारीचे वर्णन", "तक्रार",
			"अपनी शिकायत का वर्णन करें", "शिकायत का वर्णन", "शिकायत", "वर्णन",
		},
	},
	{
		key: "address",
		labels: []string{
			"enter the address or share the location where this complaint occurred",
			"address or location", "where this complaint occurred",
			"address", "location",
			"जिथे ही तक्रार घडली आहे त्या ठिकाणचा पत्ता टाका किंवा लोकेशन शेयर करा",
			"ठिकाणचा पत्ता", "पत्ता", "लोकेशन",
			"वह पता दर्ज करें या लोकेशन शेयर करें जहाँ यह शिकायत हुई है",
			"पता", "स्थान",
		},
	},
}

var meetingFields = []fieldDef{
	complaintFields[0], // name – same labels
	{
		key: "date",
		labels: []string{
			"preferred date", "date",
			"पसंतीची तारीख", "तारीख",
			"पसंदीदा तारीख",
		},
	},
	{
		key: "reason",
		labels: []string{
			"reason for appointment", "appointment reason", "reason",
			"भेटीचे कारण", "कारण",
			"अपॉइंटमेंट का कारण",
		},
	},
}

// parseDetails extracts labelled fields from a multi-line message.
//
// Two input styles are supported:
//  1. Labelled  – "Enter your full name: Amit Sharma" (any language labels)
//  2. Positional – one value per line, in the order of defs
//
// Returns the extracted values (keyed by fieldDef.key) and true on success.
func parseDetails(raw string, defs []fieldDef) (map[string]string, bool) {
	out := make(map[string]string, len(defs))
	current := ""
	labeled := false
	var positional []string

	for _, line := range strings.Split(raw, "\n") {
		clean := cleanLine(line)
		if clean == "" {
			continue
		}

		if key, val, ok := splitLabel(clean); ok {
			if field := matchLabel(key, defs); field != "" {
				labeled = true
				current = field
				out[field] = strings.TrimSpace(val)
				continue
			}
		}

		if current != "" {
			// Continuation of the previously matched field.
			if out[current] == "" {
				out[current] = clean
			} else {
				out[current] += " " + clean
			}
			continue
		}

		positional = append(positional, clean)
	}

	if labeled {
		for _, d := range defs {
			if strings.TrimSpace(out[d.key]) == "" {
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
			out[d.key] = positional[i]
		} else {
			out[d.key] = strings.Join(positional[i:], " ")
		}
	}
	return out, true
}

// splitLabel splits "key: value" on the first colon.
func splitLabel(line string) (key, val string, ok bool) {
	if i := strings.Index(line, ":"); i > 0 {
		return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
	}
	return "", "", false
}

// matchLabel returns the field key whose label set contains the given key.
// Comparison is exact after normalisation (lowercase, parentheticals removed,
// whitespace collapsed).
func matchLabel(key string, defs []fieldDef) string {
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
	return strings.Join(strings.Fields(b.String()), " ")
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

// asciiToDevanagariDigits converts ASCII digits (0-9) to Devanagari digits (०-९)
func asciiToDevanagariDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune('०' + (r - '0'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeDate accepts DD-MM-YYYY, DD/MM/YYYY or YYYY-MM-DD (ASCII or
// Devanagari digits) and returns the ISO form (YYYY-MM-DD) for PostgreSQL.
func normalizeDate(s string) (string, bool) {
	s = strings.TrimSpace(normalizePin(s))
	for _, layout := range []string{"02-01-2006", "02/01/2006", "2006-01-02", "02.01.2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// normalizeTicket turns user input into a canonical ticket ID: uppercase,
// no spaces/dashes; digits-only input gets the API's "TKT" prefix
// (e.g. "4821907" → "TKT4821907"). The API matches IDs case-insensitively.
func normalizeTicket(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r == ' ' || r == '-' || r == '_' || r == '.' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	t := b.String()
	if t == "" {
		return ""
	}
	allDigits := true
	for _, r := range t {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "TKT" + t
	}
	return t
}
