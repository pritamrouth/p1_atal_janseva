// Package bot implements the Ataljanseva citizen service conversation flow:
// language → PIN code → leader selection → 13-option main menu → sub-flows
// (raise complaint, track complaint, schedule meeting, information replies).
// All operations are context-aware for proper timeout/cancellation propagation.
package bot

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ataljanseva/janseva-chatbot/internal/db"
	"github.com/ataljanseva/janseva-chatbot/internal/inactivity"
	"github.com/ataljanseva/janseva-chatbot/internal/store"
	"github.com/ataljanseva/janseva-chatbot/internal/whatsapp"
)

const jobTimeout = 12 * time.Second

// Hero banner used for the language picker and the PIN-code prompt,
// and the leader profile photo come from the same assets as the original backend.
const heroBannerURL = "https://res.cloudinary.com/dkgfw2zf0/image/upload/v1781075980/8005beb1-ec05-401f-b514-19f1e00f00ab_de87md.png"

// meetingSlots are the fixed appointment time slots offered after the
// meeting details have been captured.
var meetingSlots = [][2]string{ // {id, label}
	{"slot_1000", "10:00 AM"},
	{"slot_1200", "12:00 PM"},
	{"slot_1500", "3:00 PM"},
	{"slot_1700", "5:00 PM"},
}

// Handler processes inbound WhatsApp messages.
type Handler struct {
	wa       whatsapp.Messenger
	store    *store.Store
	repo     *db.Repo
	inactive *inactivity.Monitor
}

func New(wa whatsapp.Messenger, s *store.Store, repo *db.Repo, inactive *inactivity.Monitor) *Handler {
	return &Handler{wa: wa, store: s, repo: repo, inactive: inactive}
}

// HandleRaw is the worker entry-point: one call per inbound message.
func (h *Handler) HandleRaw(msg whatsapp.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	log := slog.With("phone", msg.From, "type", msg.Type)
	phone := msg.From

	h.inactive.Touch(ctx, phone)

	sess, err := h.store.Get(ctx, phone)
	if err != nil {
		log.Error("get session", "err", err)
		return
	}

	if msg.Type == "text" && strings.EqualFold(strings.TrimSpace(msg.Text.Body), "reset") {
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
		if msg.Type == "text" {
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
		if msg.Type == "text" {
			h.handleComplaintDetails(ctx, phone, sess, msg.Text.Body)
		} else {
			h.promptComplaint(ctx, phone, sess)
		}

	case store.StepTrackTicket:
		if msg.Type == "text" {
			h.handleTrackTicket(ctx, phone, sess, msg.Text.Body)
		} else {
			_ = h.wa.SendText(ctx, phone, h.t(sess).TrackPrompt)
		}

	case store.StepMeeting:
		if msg.Type == "text" {
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
func (h *Handler) sendLeaderList(ctx context.Context, phone string, sess *store.Session, leaders []db.Nagarsevak) {
	t := h.t(sess)

	rows := make([][3]string, 0, len(leaders))
	for _, ns := range leaders {
		rows = append(rows, [3]string{"ns_" + ns.ID, displayName(sess.Lang, ns), partyName(sess.Lang)})
	}

	bodyText := fmt.Sprintf(t.LeaderPrompt, h.displayPin(sess, sess.Pincode))
	if err := h.wa.SendList(ctx, phone, bodyText, t.SelectLeader, []whatsapp.ListSection{
		{Title: t.LeaderSection, Rows: rows},
	}); err != nil {
		slog.Error("sendLeaderList", "phone", phone, "err", err)
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
		time.Sleep(300 * time.Millisecond)
	} else {
		_ = h.wa.SendText(ctx, phone, heading)
	}

	h.sendMenuList(ctx, phone, sess)
}

// sendMenuList sends the 13 options as a WhatsApp list message.
// WhatsApp allows max 10 rows per section, so the options are split
// into "1–10" and "11–13".
func (h *Handler) sendMenuList(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)
	rows := menuRows(t)

	sections := []whatsapp.ListSection{
		{Title: t.SectionOne, Rows: rows[:10]},
	}
	if len(rows) > 10 {
		sections = append(sections, whatsapp.ListSection{Title: t.SectionTwo, Rows: rows[10:]})
	}

	if err := h.wa.SendList(ctx, phone, t.MenuInstruction, t.MenuButton, sections); err != nil {
		slog.Error("sendMenuList", "phone", phone, "err", err)
	}
}

func (h *Handler) handleMenuSelection(ctx context.Context, phone string, sess *store.Session, ir *whatsapp.InteractiveReply) {
	id := listID(ir)
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

	case 4, 5, 6, 7, 8, 9, 10, 11, 12, 13:
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

	ticketID, err := h.repo.CreateComplaint(ctx, &db.Complaint{
		Phone:       phone,
		Lang:        sess.Lang,
		FullName:    details["name"],
		Description: details["description"],
		Address:     details["address"],
		LeaderID:    sess.NagarsevakID,
		Pincode:     sess.Pincode,
	})
	if err != nil {
		slog.Error("CreateComplaint", "phone", phone, "err", err)
		_ = h.wa.SendText(ctx, phone, t.InvalidDetails+"\n\n"+t.ComplaintPrompt)
		return
	}

	_ = h.wa.SendText(ctx, phone, fmt.Sprintf(t.ComplaintSuccess, ticketID))
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

	status, err := h.repo.GetComplaintStatus(ctx, ticketID)
	if err == sql.ErrNoRows {
		_ = h.wa.SendText(ctx, phone, t.TrackInvalid)
		return
	}
	if err != nil {
		slog.Error("GetComplaintStatus", "ticket_id", ticketID, "err", err)
		_ = h.wa.SendText(ctx, phone, t.TrackInvalid)
		return
	}

	if status == "resolved" {
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
	if !ok {
		_ = h.wa.SendText(ctx, phone, t.InvalidDetails+"\n\n"+t.MeetingPrompt)
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

	h.sendSlotList(ctx, phone, sess)
}

func (h *Handler) sendSlotList(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)

	rows := make([][3]string, 0, len(meetingSlots))
	for _, s := range meetingSlots {
		rows = append(rows, [3]string{s[0], s[1], ""})
	}

	if err := h.wa.SendList(ctx, phone, t.MeetingSlots, t.SlotsButton, []whatsapp.ListSection{
		{Title: t.SlotsSection, Rows: rows},
	}); err != nil {
		slog.Error("sendSlotList", "phone", phone, "err", err)
	}
}

func (h *Handler) handleMeetingSlot(ctx context.Context, phone string, sess *store.Session, ir *whatsapp.InteractiveReply) {
	t := h.t(sess)

	id := listID(ir)
	var slotLabel string
	for _, s := range meetingSlots {
		if s[0] == id {
			slotLabel = s[1]
			break
		}
	}
	if slotLabel == "" {
		h.sendSlotList(ctx, phone, sess)
		return
	}

	err := h.repo.CreateMeeting(ctx, &db.Meeting{
		Phone:         phone,
		Lang:          sess.Lang,
		FullName:      sess.PendingName,
		PreferredDate: sess.PendingDate,
		Reason:        sess.PendingReason,
		Slot:          slotLabel,
		LeaderID:      sess.NagarsevakID,
		Pincode:       sess.Pincode,
	})
	if err != nil {
		slog.Error("CreateMeeting", "phone", phone, "err", err)
		_ = h.wa.SendText(ctx, phone, t.InvalidDetails+"\n\n"+t.MeetingSlots)
		h.sendSlotList(ctx, phone, sess)
		return
	}

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

// menuRows builds the 13 main-menu list rows in the session language.
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
