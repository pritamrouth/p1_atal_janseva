package bot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ataljanseva/janseva-chatbot/internal/api"
)

// maxInfoItems is how many records of an API listing are shown in one message.
const maxInfoItems = 5

// errNoItems signals an API listing that is reachable but empty, so the caller
// falls back to the static localised text.
var errNoItems = errors.New("no items to show")

// numbered renders up to max items as numbered blocks ("1. …").
func numbered(items []api.Item, max int, line func(api.Item) string) []string {
	if len(items) > max {
		items = items[:max]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		body := strings.TrimSpace(line(it))
		if body == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%d. %s", len(out)+1, body))
	}
	return out
}

// listMessage assembles header + numbered lines + optional footer.
func listMessage(header string, lines []string, footer string) string {
	var b strings.Builder
	b.WriteString(header)
	for _, l := range lines {
		b.WriteString("\n\n")
		b.WriteString(l)
	}
	if footer != "" {
		b.WriteString("\n\n")
		b.WriteString(footer)
	}
	return b.String()
}

// formatGrievance renders one public-grievance record:
//
//  1. TKT4821907 · Water · 12 · Sunita Jadhav
//     Work started on the pipeline.
func formatGrievance(it api.Item) string {
	var parts []string
	for _, p := range []string{
		api.FirstStr(it, "ticketId", "ticketID", "id"),
		api.FirstStr(it, "category", "type"),
		api.FirstStr(it, "ward"),
		api.FirstStr(it, "assignedTo", "volunteerName", "assignedToName"),
	} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	line := strings.Join(parts, " · ")
	if note := api.Snippet(api.FirstStr(it, "resolutionNote", "note", "description"), 140); note != "" {
		line += "\n" + note
	}
	return line
}

// formatInfoItem renders one generic listing record (event, scheme, report,
// campaign, local service): title, then meta (date · time · location), then a
// short description — each part only when present.
func formatInfoItem(it api.Item) string {
	title := api.FirstStr(it,
		"title", "name", "heading", "label", "subject",
		"schemeName", "eventName", "reportName", "serviceName", "campaign")
	if title == "" {
		title = "—"
	}
	meta := joinNonEmpty(" · ",
		api.FirstStr(it, "date", "eventDate", "scheduledDate", "fromDate", "reportDate"),
		api.FirstStr(it, "time", "startTime", "eventTime"),
		api.FirstStr(it, "location", "venue", "place", "address"))

	desc := api.Snippet(api.FirstStr(it,
		"description", "details", "about", "summary", "note", "notes", "content", "work"), 140)
	if desc == title {
		desc = ""
	}

	var b strings.Builder
	b.WriteString(title)
	if meta != "" {
		b.WriteString("\n" + meta)
	}
	if desc != "" {
		b.WriteString("\n" + desc)
	}
	return b.String()
}

// formatProfile renders a political-user profile as the About-leader message
// and returns its photo URL ("" when none). fallbackName is the leader's name
// from our own database, used when the profile carries none.
func formatProfile(it api.Item, fallbackName string) (text, photo string) {
	name := api.FirstStr(it, "name", "fullName", "displayName", "username", "title")
	if name == "" {
		name = fallbackName
	}
	role := api.FirstStr(it, "designation", "position", "role", "post")
	party := api.FirstStr(it, "party", "partyName", "organization", "affiliation")
	bio := api.Snippet(api.FirstStr(it,
		"bio", "about", "description", "summary", "intro"), 600)
	photo = api.FirstStr(it, "photoUrl", "photoURL", "profilePhoto", "photo", "image", "imageUrl")

	var b strings.Builder
	b.WriteString("🎖️ " + name)
	if line := joinNonEmpty(" · ", role, party); line != "" {
		b.WriteString("\n" + line)
	}
	if bio != "" && bio != name {
		b.WriteString("\n\n" + bio)
	}
	return b.String(), photo
}

func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
