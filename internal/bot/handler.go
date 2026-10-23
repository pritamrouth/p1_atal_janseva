// Package bot implements the Ataljanseva citizen service conversation flow:
// language → PIN code → leader selection → 13-option main menu → sub-flows
// (raise complaint, track complaint, schedule meeting, information replies).
// Complaints, tracking, appointments and info listings call the public
// AtalJanseva REST API (internal/api); leaders come from PostgreSQL.
// All operations are context-aware for proper timeout/cancellation propagation.
package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ataljanseva/janseva-chatbot/internal/api"
	"github.com/ataljanseva/janseva-chatbot/internal/db"
	"github.com/ataljanseva/janseva-chatbot/internal/inactivity"
	"github.com/ataljanseva/janseva-chatbot/internal/store"
	"github.com/ataljanseva/janseva-chatbot/internal/whatsapp"
)

// jobTimeout budgets one inbound message end-to-end. It must comfortably fit
// a DB lookup + an AtalJanseva API call + WhatsApp API call (Meta downloads
// the leader photo itself, which can be slow on first send) + a retry.
const jobTimeout = 30 * time.Second

// Hero banner used for the language picker and the PIN-code prompt,
// and the leader profile photo come from the same assets as the original backend.
const heroBannerURL = "https://res.cloudinary.com/dkgfw2zf0/image/upload/v1781075980/8005beb1-ec05-401f-b514-19f1e00f00ab_de87md.png"

// meetingSlots are the fixed candidate slots offered after the meeting
// details have been captured: {row id, label, 24h time}. Booked ones are
// filtered out against GET /api/schedule/appointments/slots.
var meetingSlots = [][3]string{
	{"slot_1000", "10:00 AM", "10:00"},
	{"slot_1200", "12:00 PM", "12:00"},
	{"slot_1500", "3:00 PM", "15:00"},
	{"slot_1700", "5:00 PM", "17:00"},
}

// Handler processes inbound WhatsApp messages.
type Handler struct {
	wa       whatsapp.Messenger
	store    *store.Store
	repo     *db.Repo
	inactive *inactivity.Monitor
	api      *api.Client
}

func New(wa whatsapp.Messenger, s *store.Store, repo *db.Repo, inactive *inactivity.Monitor, apiClient *api.Client) *Handler {
	return &Handler{wa: wa, store: s, repo: repo, inactive: inactive, api: apiClient}
}

// HandleRaw is the worker entry-point: one call per inbound message.
func (h *Handler) HandleRaw(msg whatsapp.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	log := slog.With("phone", msg.From, "type", msg.Type)
	phone := msg.From

	// Meta delivers webhooks at-least-once: the same WhatsApp message ID can
	// arrive twice (or be retried). Claim it before doing any work, otherwise
	// duplicate copies run concurrently and send duplicate replies.
	if first, err := h.store.ClaimMessage(ctx, msg.ID); err != nil {
		log.Warn("message dedup failed, processing anyway", "id", msg.ID, "err", err)
	} else if !first {
		log.Info("duplicate message ignored", "id", msg.ID)
		return
	}

	h.inactive.Touch(ctx, phone)

	sess, err := h.store.Get(ctx, phone)
	if err != nil {
		log.Error("get session", "err", err)
		return
	}

	if msg.Type == "text" && msg.Text != nil && strings.EqualFold(strings.TrimSpace(msg.Text.Body), "reset") {
		if err := h.store.Reset(ctx, phone); err != nil {
			log.Error("reset session", "err", err)
		}
		h.inactive.Cancel(ctx, phone)
		_ = h.wa.SendText(ctx, phone, "🔄 Session reset. Type anything to start over.")
		return
	}

	switch sess.Step {

	case store.StepStart:
		h.handleStart(ctx, phone, sess, msg)

	case store.StepLangChosen:
		if msg.Type == "text" && msg.Text != nil {
			h.handlePin(ctx, phone, sess, strings.TrimSpace(msg.Text.Body))
		} else {
			_ = h.wa.SendText(ctx, phone, h.t(sess).PinPrompt)
		}

	case store.StepLeader:
		if msg.Type == "interactive" && msg.Interactive != nil {
			h.handleLeaderReply(ctx, phone, sess, msg.Interactive)
		} else {
			h.promptLeader(ctx, phone, sess)
		}

	case store.StepMainMenu:
		if msg.Type == "interactive" && msg.Interactive != nil {
			h.handleMenuSelection(ctx, phone, sess, msg.Interactive)
		} else {
			h.sendMenuList(ctx, phone, sess)
		}

	case store.StepComplaint:
		if msg.Type == "text" && msg.Text != nil {
			h.handleComplaintDetails(ctx, phone, sess, msg.Text.Body)
		} else {
			h.promptComplaint(ctx, phone, sess)
		}

	case store.StepTrackTicket:
		if msg.Type == "text" && msg.Text != nil {
			h.handleTrackTicket(ctx, phone, sess, msg.Text.Body)
		} else {
			_ = h.wa.SendText(ctx, phone, h.t(sess).TrackPrompt)
		}

	case store.StepMeeting:
		if msg.Type == "text" && msg.Text != nil {
			h.handleMeetingDetails(ctx, phone, sess, msg.Text.Body)
		} else {
			_ = h.wa.SendText(ctx, phone, h.t(sess).MeetingPrompt)
		}

	case store.StepMeetingSlot:
		if msg.Type == "interactive" && msg.Interactive != nil {
			h.handleMeetingSlot(ctx, phone, sess, msg.Interactive)
		} else {
			h.sendSlotList(ctx, phone, sess)
		}
	}
}

// ─────────────────────────────────────────────
// Step 0 – language picker
// ─────────────────────────────────────────────

func (h *Handler) handleStart(ctx context.Context, phone string, sess *store.Session, msg whatsapp.Message) {
	var lang string

	switch {
	case msg.Type == "interactive" && msg.Interactive != nil:
		switch buttonID(msg.Interactive) {
		case "lang_en":
			lang = "en"
		case "lang_mr":
			lang = "mr"
		case "lang_hi":
			lang = "hi"
		}
	case msg.Type == "text":
		if msg.Text == nil {
			break
		}
		choice := strings.TrimSpace(msg.Text.Body)
		// Button order: Marathi (1), English (2), Hindi (3)
		switch choice {
		case "lang_mr", "1", "मराठी":
			lang = "mr"
		case "lang_en", "2", "English":
			lang = "en"
		case "lang_hi", "3", "हिंदी":
			lang = "hi"
		}
	}

	if lang == "" {
		h.sendLanguagePicker(ctx, phone, phone)
		return
	}

	sess.Lang = lang
	sess.Step = store.StepLangChosen
	if err := h.store.Save(ctx, sess); err != nil {
		slog.Error("save session", "phone", phone, "err", err)
		return
	}
	h.sendPinPrompt(ctx, phone, sess)
}

func (h *Handler) sendLanguagePicker(ctx context.Context, phone, rawPhone string) {
	greeting := GreetingFor("en", rawPhone)
	if err := h.wa.SendButtonsWithImageHeader(ctx, phone, greeting, heroBannerURL, [][2]string{
		{"lang_mr", "मराठी"},
		{"lang_en", "English"},
		{"lang_hi", "हिंदी"},
	}); err != nil {
		slog.Error("sendLanguagePicker", "phone", phone, "err", err)
	}
}

func (h *Handler) sendPinPrompt(ctx context.Context, phone string, sess *store.Session) {
	if err := h.wa.SendImage(ctx, phone, heroBannerURL, h.t(sess).PinPrompt); err != nil {
		slog.Error("sendPinPrompt", "phone", phone, "err", err)
		_ = h.wa.SendText(ctx, phone, h.t(sess).PinPrompt)
	}
}

// ─────────────────────────────────────────────
// Step 1 – PIN code → leaders for the pincode
// ─────────────────────────────────────────────

func (h *Handler) handlePin(ctx context.Context, phone string, sess *store.Session, raw string) {
	pin := normalizePin(raw)

	if len([]rune(pin)) != 6 {
		h.sendInvalidPin(ctx, phone, sess, raw)
		return
	}

	result, err := h.repo.NagarsevaksByPincode(ctx, pin)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Error("NagarsevaksByPincode", "pin", pin, "err", err)
		}
		h.sendInvalidPin(ctx, phone, sess, pin)
		return
	}

	// Persist location and pincode from result.
	sess.Pincode = pin
	sess.State = result.Location.State
	sess.District = result.Location.District
	sess.StateHindi = result.Location.StateHindi
	sess.DistrictHindi = result.Location.DistrictHindi
	sess.Step = store.StepLeader

	if err := h.store.Save(ctx, sess); err != nil {
		slog.Error("save session", "phone", phone, "err", err)
		return
	}

	h.sendLeaderList(ctx, phone, sess, result.List)
}

func (h *Handler) sendInvalidPin(ctx context.Context, phone string, sess *store.Session, pin string) {
	displayPin := pin
	if sess.Lang == "mr" || sess.Lang == "hi" {
		displayPin = asciiToDevanagariDigits(pin)
	}
	_ = h.wa.SendText(ctx, phone, fmt.Sprintf(h.t(sess).InvalidPin, displayPin))
}

// ─────────────────────────────────────────────
// Step 2 – leader list
// ─────────────────────────────────────────────

func (h *Handler) promptLeader(ctx context.Context, phone string, sess *store.Session) {
	result, err := h.repo.NagarsevaksByPincode(ctx, sess.Pincode)
	if err != nil || len(result.List) == 0 {
		slog.Error("promptLeader", "pin", sess.Pincode, "err", err)
		h.sendInvalidPin(ctx, phone, sess, sess.Pincode)
		return
	}
	h.sendLeaderList(ctx, phone, sess, result.List)
}

// sendLeaderList renders the WhatsApp list message of leaders for the pincode.
// Meta caps the TOTAL row count of one list message at 10 (error #131009),
// so pin codes with more than 10 leaders are split across several messages.
func (h *Handler) sendLeaderList(ctx context.Context, phone string, sess *store.Session, leaders []db.Nagarsevak) {
	t := h.t(sess)

	rows := make([][3]string, 0, len(leaders))
	for _, ns := range leaders {
		rows = append(rows, [3]string{"ns_" + ns.ID, displayName(sess.Lang, ns), partyName(sess.Lang)})
	}

	bodyText := fmt.Sprintf(t.LeaderPrompt, h.displayPin(sess, sess.Pincode))
	for _, chunk := range chunkRows(rows) {
		sections := []whatsapp.ListSection{{Title: t.LeaderSection, Rows: chunk}}
		if err := h.sendListWithRetry(ctx, phone, bodyText, t.SelectLeader, sections); err != nil {
			slog.Error("sendLeaderList", "phone", phone, "err", err)
			return
		}
	}
}

func (h *Handler) handleLeaderReply(ctx context.Context, phone string, sess *store.Session, ir *whatsapp.InteractiveReply) {
	id := listID(ir)
	if !strings.HasPrefix(id, "ns_") {
		h.promptLeader(ctx, phone, sess)
		return
	}

	ns, err := h.repo.NagarsevakByID(ctx, strings.TrimPrefix(id, "ns_"))
	if err != nil {
		slog.Error("NagarsevakByID", "id", id, "err", err)
		h.promptLeader(ctx, phone, sess)
		return
	}

	sess.NagarsevakID = ns.ID
	sess.NagarsevakName = ns.FullName
	sess.NagarsevakSlug = ns.Slug
	sess.NagarsevakWard = ns.Ward // persisted for the IVR upsert on session expiry
	sess.Step = store.StepMainMenu
	if err := h.store.Save(ctx, sess); err != nil {
		slog.Error("save session", "phone", phone, "err", err)
		return
	}
	h.sendMainMenu(ctx, phone, sess)
}

// ─────────────────────────────────────────────
// Step 3 – main menu (13 options)
// ─────────────────────────────────────────────

func (h *Handler) sendMainMenu(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)

	ns, err := h.repo.NagarsevakByID(ctx, sess.NagarsevakID)
	if err != nil {
		slog.Error("sendMainMenu: NagarsevakByID", "phone", phone, "err", err)
		ns = &db.Nagarsevak{FullName: sess.NagarsevakName}
	}

	heading := fmt.Sprintf(t.MenuHeading, displayName(sess.Lang, *ns))
	if ns.ProfilePhoto != "" {
		if err := h.wa.SendImage(ctx, phone, ns.ProfilePhoto, heading); err != nil {
			slog.Warn("sendMainMenu: profile photo failed", "phone", phone, "url", ns.ProfilePhoto, "err", err)
			_ = h.wa.SendText(ctx, phone, heading)
		}
	} else {
		_ = h.wa.SendText(ctx, phone, heading)
	}

	h.sendMenuList(ctx, phone, sess)
}

// metaListMaxRows is Meta's maximum TOTAL row count for a single list
// message across all of its sections. Exceeding it fails with API error
// #131009 "Total row count exceed max allowed count: 10".
const metaListMaxRows = 10

// sendMenuList sends the main list: options 1–9 plus a "More Options"
// row (Meta caps one list message at 10 rows TOTAL, and flow.txt has 13
// options). Tapping "More Options" sends the remaining options 10–13.
// On failure it retries once with a fresh context (the original ctx may be
// nearly exhausted after the leader image upload) — losing the menu would
// strand the user with no way to pick an option.
func (h *Handler) sendMenuList(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)
	sections := []whatsapp.ListSection{{Title: t.SectionOne, Rows: primaryMenuRows(t)}}

	if err := h.sendListWithRetry(ctx, phone, t.MenuInstruction, t.MenuButton, sections); err != nil {
		slog.Error("sendMenuList failed", "phone", phone, "err", err)
		return
	}
	slog.Info("menu list sent", "phone", phone, "lang", sess.Lang, "step", sess.Step)
}

// sendMoreOptionsList sends options 10–13, shown after the user taps the
// "More Options" row of the main menu list.
func (h *Handler) sendMoreOptionsList(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)
	sections := []whatsapp.ListSection{{Title: t.SectionTwo, Rows: secondaryMenuRows(t)}}

	if err := h.sendListWithRetry(ctx, phone, t.MenuInstruction, t.MenuButton, sections); err != nil {
		slog.Error("sendMoreOptionsList failed", "phone", phone, "err", err)
		return
	}
	slog.Info("more options list sent", "phone", phone, "lang", sess.Lang)
}

// sendListWithRetry sends one list message; if the (possibly nearly
// exhausted) ctx fails, it retries once with a fresh context.
func (h *Handler) sendListWithRetry(ctx context.Context, phone, body, button string, sections []whatsapp.ListSection) error {
	if err := h.wa.SendList(ctx, phone, body, button, sections); err != nil {
		slog.Warn("SendList failed, retrying with fresh context", "phone", phone, "err", err)

		retryCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := h.wa.SendList(retryCtx, phone, body, button, sections); err != nil {
			return err
		}
	}
	return nil
}

// chunkRows splits rows into chunks of at most metaListMaxRows,
// so each chunk can be sent as its own list message.
func chunkRows(rows [][3]string) [][][3]string {
	var chunks [][][3]string
	for start := 0; start < len(rows); start += metaListMaxRows {
		end := start + metaListMaxRows
		if end > len(rows) {
			end = len(rows)
		}
		chunks = append(chunks, rows[start:end])
	}
	return chunks
}

func (h *Handler) handleMenuSelection(ctx context.Context, phone string, sess *store.Session, ir *whatsapp.InteractiveReply) {
	id := listID(ir)

	if id == "opt_more" {
		// Reveal the remaining options (10–13).
		h.sendMoreOptionsList(ctx, phone, sess)
		return
	}

	if !strings.HasPrefix(id, "opt_") {
		h.sendMenuList(ctx, phone, sess)
		return
	}

	var n int
	if _, err := fmt.Sscanf(id, "opt_%d", &n); err != nil || n < 1 || n > 13 {
		h.sendMenuList(ctx, phone, sess)
		return
	}
	h.dispatchOption(ctx, phone, sess, n)
}

// dispatchOption routes one of the 13 main-menu options.
func (h *Handler) dispatchOption(ctx context.Context, phone string, sess *store.Session, n int) {
	t := h.t(sess)

	switch n {
	case 1: // Raise complaint
		sess.Step = store.StepComplaint
		if err := h.store.Save(ctx, sess); err != nil {
			slog.Error("save session", "phone", phone, "err", err)
			return
		}
		h.promptComplaint(ctx, phone, sess)

	case 2: // Track complaint
		sess.Step = store.StepTrackTicket
		if err := h.store.Save(ctx, sess); err != nil {
			slog.Error("save session", "phone", phone, "err", err)
			return
		}
		_ = h.wa.SendText(ctx, phone, t.TrackPrompt)

	case 3: // Schedule meeting
		sess.Step = store.StepMeeting
		if err := h.store.Save(ctx, sess); err != nil {
			slog.Error("save session", "phone", phone, "err", err)
			return
		}
		_ = h.wa.SendText(ctx, phone, t.MeetingPrompt)

	case 4, 5, 6, 7, 8, 9, 12:
		// API-backed information (About leader, Transparency Board, Work
		// Reports, Events, Schemes, JanSeva Connect, Youth Campaigns).
		h.sendDynamicOption(ctx, phone, sess, n)
		h.sendMenuList(ctx, phone, sess)

	case 10, 11, 13:
		// No matching API endpoint (NariShakti, Swadeshi, Join Our Team) —
		// static localised text.
		h.sendInfoReply(ctx, phone, sess, n)
		h.sendMenuList(ctx, phone, sess)

	default:
		h.sendMenuList(ctx, phone, sess)
	}
}

// sendInfoReply sends the static localised text for options 4–13,
// followed by the closing "Thank you" line.
func (h *Handler) sendInfoReply(ctx context.Context, phone string, sess *store.Session, n int) {
	t := h.t(sess)
	leader := h.leaderDisplayName(ctx, sess)

	var text string
	switch n {
	case 4:
		text = fmt.Sprintf(t.AboutLeaderText, leader)
	case 5:
		text = t.TransparencyText
	case 6:
		text = t.WorkReportsText
	case 7:
		text = t.EventsText
	case 8:
		text = t.SchemesText
	case 9:
		text = t.JanSevaText
	case 10:
		text = t.NariShaktiText
	case 11:
		text = t.SwadeshiText
	case 12:
		text = t.YouthText
	case 13:
		text = t.JoinTeamText
	default:
		return
	}

	_ = h.wa.SendText(ctx, phone, text+"\n\n"+t.ThankYou)
}

// ─────────────────────────────────────────────
// API-backed info options (4–9, 12)
// ─────────────────────────────────────────────

// sendDynamicOption sends the API-backed content for one menu option,
// falling back to the static localised text when the API fails, returns
// nothing, or the option has no endpoint.
func (h *Handler) sendDynamicOption(ctx context.Context, phone string, sess *store.Session, n int) {
	t := h.t(sess)

	text, photo, err := h.dynamicOption(ctx, sess, n)
	if err != nil {
		slog.Warn("dynamic option: falling back to static text",
			"option", n, "phone", phone, "err", err)
		h.sendInfoReply(ctx, phone, sess, n)
		return
	}

	msg := text + "\n\n" + t.ThankYou
	if photo != "" {
		if err := h.wa.SendImage(ctx, phone, photo, msg); err != nil {
			slog.Warn("dynamic option: image failed", "option", n, "err", err)
			_ = h.wa.SendText(ctx, phone, msg)
		}
		return
	}
	_ = h.wa.SendText(ctx, phone, msg)
}

// dynamicOption fetches and formats the API content for one menu option.
// The returned photo ("", when none) is sent as an image caption if set.
func (h *Handler) dynamicOption(ctx context.Context, sess *store.Session, n int) (text, photo string, err error) {
	t := h.t(sess)
	slug := sess.NagarsevakSlug

	switch n {
	case 4: // About leader
		prof, ferr := h.api.ProfileBySlug(ctx, slug)
		if ferr != nil {
			return "", "", ferr
		}
		leader := h.leaderDisplayName(ctx, sess)
		text, photo = formatProfile(prof, leader)
		return text + "\n\n" + fmt.Sprintf(t.AboutLeaderText, leader), photo, nil

	case 5: // Transparency Board – resolved grievances of this office
		return h.listOption(ctx, sess, t.TransparencyHeader, "/api/grievances/public/all",
			url.Values{
				"nagarsevakId": {slug},
				"status":       {"Resolved"},
				"limit":        {strconv.Itoa(maxInfoItems)},
			}, formatGrievance)

	case 6:
		return h.listOption(ctx, sess, t.WorkReportsHeader, "/api/work-reports/public",
			limitQuery(), formatInfoItem)

	case 7:
		return h.listOption(ctx, sess, t.EventsHeader, "/api/events/public",
			limitQuery(), formatInfoItem)

	case 8:
		return h.listOption(ctx, sess, t.SchemesHeader, "/api/programs/public",
			limitQuery(), formatInfoItem)

	case 9:
		return h.listOption(ctx, sess, t.JanSevaHeader, "/api/local-services/public",
			limitQuery(), formatInfoItem)

	case 12:
		return h.listOption(ctx, sess, t.YouthHeader, "/api/campaigns/public",
			limitQuery(), formatInfoItem)
	}
	return "", "", fmt.Errorf("option %d has no API-backed content", n)
}

func limitQuery() url.Values {
	return url.Values{"limit": {strconv.Itoa(maxInfoItems)}}
}

// listOption fetches a listing endpoint and formats it as one message:
// header, up to maxInfoItems numbered items and the "visit ataljanseva.in"
// footer. An empty listing returns errNoItems so the caller can fall back.
func (h *Handler) listOption(
	ctx context.Context,
	sess *store.Session,
	header, path string,
	q url.Values,
	line func(api.Item) string,
) (text, photo string, err error) {
	items, ferr := h.api.PublicList(ctx, path, q)
	if ferr != nil {
		return "", "", ferr
	}
	lines := numbered(items, maxInfoItems, line)
	if len(lines) == 0 {
		return "", "", errNoItems
	}
	return listMessage(header, lines, h.t(sess).InfoMoreLink), "", nil
}

// ─────────────────────────────────────────────
// Option 1 – raise complaint
// ─────────────────────────────────────────────

func (h *Handler) promptComplaint(ctx context.Context, phone string, sess *store.Session) {
	_ = h.wa.SendText(ctx, phone, h.t(sess).ComplaintPrompt)
}

func (h *Handler) handleComplaintDetails(ctx context.Context, phone string, sess *store.Session, raw string) {
	t := h.t(sess)

	details, ok := parseDetails(raw, complaintFields)
	if !ok {
		_ = h.wa.SendText(ctx, phone, t.InvalidDetails+"\n\n"+t.ComplaintPrompt)
		return
	}

	g, err := h.api.CreateGrievance(ctx, api.GrievanceRequest{
		NagarsevakID: sess.NagarsevakSlug,
		Name:         details["name"],
		Mobile:       api.Mobile10(phone),
		Description:  details["description"],
		Address:      details["address"],
		Ward:         sess.NagarsevakWard,
	})
	if err != nil {
		slog.Error("CreateGrievance", "phone", phone, "err", err)
		_ = h.wa.SendText(ctx, phone, t.ComplaintFailed+"\n\n"+t.ComplaintPrompt)
		return
	}
	if g.TicketID == "" {
		slog.Error("CreateGrievance: empty ticket ID", "phone", phone)
		_ = h.wa.SendText(ctx, phone, t.ComplaintFailed+"\n\n"+t.ComplaintPrompt)
		return
	}

	_ = h.wa.SendText(ctx, phone, fmt.Sprintf(t.ComplaintSuccess, g.TicketID))
	h.returnToMenu(ctx, phone, sess)
}

// ─────────────────────────────────────────────
// Option 2 – track complaint
// ─────────────────────────────────────────────

func (h *Handler) handleTrackTicket(ctx context.Context, phone string, sess *store.Session, raw string) {
	t := h.t(sess)

	ticketID := normalizeTicket(raw)
	if ticketID == "" {
		_ = h.wa.SendText(ctx, phone, t.TrackInvalid)
		return
	}

	g, err := h.api.TrackTicket(ctx, ticketID)
	if err != nil {
		if api.IsNotFound(err) {
			_ = h.wa.SendText(ctx, phone, t.TrackInvalid)
			return
		}
		slog.Error("TrackTicket", "ticket_id", ticketID, "err", err)
		_ = h.wa.SendText(ctx, phone, t.TrackFailed)
		return
	}

	// The API reports "Pending" / "In Progress" / "Resolved"; flow.txt has
	// only two messages, so everything short of Resolved is "in progress".
	if g.TicketID != "" {
		ticketID = g.TicketID
	}
	if strings.EqualFold(strings.TrimSpace(g.Status), "resolved") {
		_ = h.wa.SendText(ctx, phone, fmt.Sprintf(t.TrackResolved, ticketID))
	} else {
		_ = h.wa.SendText(ctx, phone, fmt.Sprintf(t.TrackInProgress, ticketID))
	}
	h.returnToMenu(ctx, phone, sess)
}

// ─────────────────────────────────────────────
// Option 3 – schedule meeting
// ─────────────────────────────────────────────

func (h *Handler) handleMeetingDetails(ctx context.Context, phone string, sess *store.Session, raw string) {
	t := h.t(sess)

	details, ok := parseDetails(raw, meetingFields)
	if !ok {
		_ = h.wa.SendText(ctx, phone, t.InvalidDetails+"\n\n"+t.MeetingPrompt)
		return
	}

	dateISO, ok := normalizeDate(details["date"])
	if !ok || isPastDate(dateISO) {
		_ = h.wa.SendText(ctx, phone, t.InvalidDetails+"\n\n"+t.MeetingPrompt)
		return
	}

	// Only offer slots the office still has free for this date.
	slots := h.availableSlots(ctx, sess, dateISO)
	if len(slots) == 0 {
		_ = h.wa.SendText(ctx, phone, t.MeetingNoSlots+"\n\n"+t.MeetingPrompt)
		return
	}

	sess.PendingName = details["name"]
	sess.PendingDate = dateISO
	sess.PendingReason = details["reason"]
	sess.Step = store.StepMeetingSlot
	if err := h.store.Save(ctx, sess); err != nil {
		slog.Error("save session", "phone", phone, "err", err)
		return
	}

	h.renderSlotList(ctx, phone, sess, slots)
}

// availableSlots returns the candidate slots still free on dateISO.
// When the slots lookup fails the fixed candidates are offered anyway —
// a conflict then surfaces at booking time and re-opens the list.
func (h *Handler) availableSlots(ctx context.Context, sess *store.Session, dateISO string) [][3]string {
	booked, err := h.api.BookedSlots(ctx, sess.NagarsevakSlug, dateISO)
	if err != nil {
		slog.Warn("BookedSlots", "slug", sess.NagarsevakSlug, "date", dateISO, "err", err)
		booked = nil
	}
	return freeMeetingSlots(booked)
}

// freeMeetingSlots filters the fixed candidates against the booked set.
func freeMeetingSlots(booked map[string]bool) [][3]string {
	out := make([][3]string, 0, len(meetingSlots))
	for _, s := range meetingSlots {
		if !booked[s[2]] {
			out = append(out, s)
		}
	}
	return out
}

// slotByID returns the candidate slot with the given row id, or nil.
func slotByID(id string) *[3]string {
	for i := range meetingSlots {
		if meetingSlots[i][0] == id {
			return &meetingSlots[i]
		}
	}
	return nil
}

// isPastDate reports whether the ISO date is before today (the API rejects
// past dates).
func isPastDate(iso string) bool {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return true
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return t.Before(today)
}

func (h *Handler) sendSlotList(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)

	// The draft expired (or was never stored) — restart the meeting flow.
	if sess.PendingDate == "" {
		sess.Step = store.StepMeeting
		if err := h.store.Save(ctx, sess); err != nil {
			slog.Error("save session", "phone", phone, "err", err)
		}
		_ = h.wa.SendText(ctx, phone, t.MeetingPrompt)
		return
	}

	slots := h.availableSlots(ctx, sess, sess.PendingDate)
	if len(slots) == 0 {
		_ = h.wa.SendText(ctx, phone, t.MeetingNoSlots+"\n\n"+t.MeetingPrompt)
		sess.Step = store.StepMeeting
		if err := h.store.Save(ctx, sess); err != nil {
			slog.Error("save session", "phone", phone, "err", err)
		}
		return
	}
	h.renderSlotList(ctx, phone, sess, slots)
}

func (h *Handler) renderSlotList(ctx context.Context, phone string, sess *store.Session, slots [][3]string) {
	t := h.t(sess)

	rows := make([][3]string, 0, len(slots))
	for _, s := range slots {
		rows = append(rows, [3]string{s[0], s[1], ""})
	}

	if err := h.sendListWithRetry(ctx, phone, t.MeetingSlots, t.SlotsButton, []whatsapp.ListSection{
		{Title: t.SlotsSection, Rows: rows},
	}); err != nil {
		slog.Error("renderSlotList", "phone", phone, "err", err)
	}
}

func (h *Handler) handleMeetingSlot(ctx context.Context, phone string, sess *store.Session, ir *whatsapp.InteractiveReply) {
	t := h.t(sess)

	slot := slotByID(listID(ir))
	if slot == nil {
		h.sendSlotList(ctx, phone, sess)
		return
	}

	appt, err := h.api.BookAppointment(ctx, api.BookRequest{
		NagarsevakID: sess.NagarsevakSlug,
		Name:         sess.PendingName,
		Phone:        api.Mobile10(phone),
		Date:         sess.PendingDate,
		Time:         slot[2],
		Reason:       sess.PendingReason,
	})

	switch {
	case api.IsSlotTaken(err):
		// Someone else took the slot — re-fetch and offer what's left.
		_ = h.wa.SendText(ctx, phone, t.MeetingSlotTaken)
		h.sendSlotList(ctx, phone, sess)
		return
	case err != nil:
		slog.Error("BookAppointment", "phone", phone, "date", sess.PendingDate, "time", slot[2], "err", err)
		_ = h.wa.SendText(ctx, phone, t.MeetingFailed)
		h.sendSlotList(ctx, phone, sess)
		return
	}

	// The API sends no confirmation message itself — the bot sends its own.
	slog.Info("appointment booked",
		"phone", phone,
		"name", sess.PendingName,
		"date", appt.Date,
		"time", appt.Time,
		"end", appt.EndTime,
		"id", appt.ID,
	)
	_ = h.wa.SendText(ctx, phone, t.MeetingSuccess)
	h.returnToMenu(ctx, phone, sess)
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

// returnToMenu moves an active session back to the main menu and re-sends
// the option list (without repeating the leader banner).
func (h *Handler) returnToMenu(ctx context.Context, phone string, sess *store.Session) {
	sess.Step = store.StepMainMenu
	sess.PendingName = ""
	sess.PendingDate = ""
	sess.PendingReason = ""
	if err := h.store.Save(ctx, sess); err != nil {
		slog.Error("save session", "phone", phone, "err", err)
		return
	}
	h.sendMenuList(ctx, phone, sess)
}

func (h *Handler) t(sess *store.Session) Strings {
	if t, ok := I18n[sess.Lang]; ok {
		return t
	}
	return I18n["en"]
}

// leaderDisplayName returns the leader's name in the session language,
// falling back to the session-cached name.
func (h *Handler) leaderDisplayName(ctx context.Context, sess *store.Session) string {
	ns, err := h.repo.NagarsevakByID(ctx, sess.NagarsevakID)
	if err != nil {
		return sess.NagarsevakName
	}
	return displayName(sess.Lang, *ns)
}

// displayName picks the language-appropriate name for a leader.
func displayName(lang string, ns db.Nagarsevak) string {
	if (lang == "mr" || lang == "hi") && ns.NameHindi != "" {
		return ns.NameHindi
	}
	return ns.FullName
}

// displayPin converts the pincode to Devanagari numerals for Hindi/Marathi.
func (h *Handler) displayPin(sess *store.Session, pin string) string {
	if sess.Lang == "mr" || sess.Lang == "hi" {
		return asciiToDevanagariDigits(pin)
	}
	return pin
}

// menuRows builds the 13 main-menu rows in the session language.
func menuRows(t Strings) [][3]string {
	return [][3]string{
		{"opt_1", t.OptRaiseComplaint, ""},
		{"opt_2", t.OptTrackComplaint, ""},
		{"opt_3", t.OptScheduleMeeting, ""},
		{"opt_4", t.OptAboutLeader, ""},
		{"opt_5", t.OptTransparency, ""},
		{"opt_6", t.OptWorkReports, ""},
		{"opt_7", t.OptEvents, ""},
		{"opt_8", t.OptSchemes, ""},
		{"opt_9", t.OptJanSeva, ""},
		{"opt_10", t.OptNariShakti, ""},
		{"opt_11", t.OptSwadeshi, ""},
		{"opt_12", t.OptYouth, ""},
		{"opt_13", t.OptJoinTeam, ""},
	}
}

// primaryMenuRows returns what fits in the ONE main list message:
// options 1–9 plus the "More Options" row (10 rows = Meta's hard limit).
func primaryMenuRows(t Strings) [][3]string {
	rows := menuRows(t)
	primary := make([][3]string, 0, metaListMaxRows)
	primary = append(primary, rows[:9]...)
	primary = append(primary, [3]string{"opt_more", t.OptMoreOptions, ""})
	return primary
}

// secondaryMenuRows returns the options revealed by "More Options": 10–13.
func secondaryMenuRows(t Strings) [][3]string {
	return menuRows(t)[9:]
}

func buttonID(ir *whatsapp.InteractiveReply) string {
	if ir == nil {
		return ""
	}
	if ir.ButtonReply != nil {
		return ir.ButtonReply.ID
	}
	if ir.ListReply != nil {
		return ir.ListReply.ID
	}
	return ""
}

func listID(ir *whatsapp.InteractiveReply) string {
	if ir == nil || ir.ListReply == nil {
		return ""
	}
	return ir.ListReply.ID
}
