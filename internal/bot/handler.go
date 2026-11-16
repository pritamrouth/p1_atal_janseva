// Package bot implements the Ataljanseva citizen service conversation flow:
// preselected leader's 10-option main menu (Marathi) → sub-flows (raise
// complaint, track complaint, schedule meeting, information replies).
// The leader is configured at startup (LEADER_ID / LEADER_SLUG) and read from
// political_users, so citizens no longer enter a PIN code, choose a leader, or
// pick a language. Complaints, tracking and appointments call the public
// AtalJanseva REST API (internal/api); info options redirect to ataljanseva.in.
// All operations are context-aware for proper timeout/cancellation propagation.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"janseva_chatbot/internal/api"
	"janseva_chatbot/internal/db"
	"janseva_chatbot/internal/inactivity"
	"janseva_chatbot/internal/storage"
	"janseva_chatbot/internal/store"
	"janseva_chatbot/internal/whatsapp"
)

// jobTimeout budgets one inbound message end-to-end. It must comfortably fit
// a DB lookup + an AtalJanseva API call + WhatsApp API call (Meta downloads
// the leader photo itself, which can be slow on first send) + a retry. Voice
// complaints add a Meta media download and an S3 upload.
const jobTimeout = 60 * time.Second

// defaultLang is Marathi: the language picker was removed, so every session
// starts in Marathi.
const defaultLang = "mr"

// meetingSlots are the fixed candidate slots offered after the meeting
// details have been captured: {row id, label, 24h time}. Booked ones are
// filtered out against GET /api/schedule/appointments/slots.
var meetingSlots = [][3]string{
	{"slot_1000", "10:00 AM", "10:00"},
	{"slot_1200", "12:00 PM", "12:00"},
	{"slot_1500", "3:00 PM", "15:00"},
	{"slot_1700", "5:00 PM", "17:00"},
}

// audioUploader stores a voice note and returns its public URL.
type audioUploader interface {
	Upload(ctx context.Context, key string, body []byte, contentType string) (string, error)
}

// Handler processes inbound WhatsApp messages.
type Handler struct {
	wa         whatsapp.Messenger
	store      *store.Store
	repo       *db.Repo
	inactive   *inactivity.Monitor
	api        *api.Client
	audio      audioUploader // nil when S3 is not configured
	leaderID   string        // optional UUID override
	leaderSlug string        // used when leaderID is empty
}

func New(
	wa whatsapp.Messenger,
	s *store.Store,
	repo *db.Repo,
	inactive *inactivity.Monitor,
	apiClient *api.Client,
	audio *storage.Uploader,
	leaderID, leaderSlug string,
) *Handler {
	h := &Handler{
		wa:         wa,
		store:      s,
		repo:       repo,
		inactive:   inactive,
		api:        apiClient,
		leaderID:   leaderID,
		leaderSlug: leaderSlug,
	}
	if audio != nil {
		h.audio = audio
	}
	return h
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

	// A quick double tap sends two messages with different IDs but the same
	// row. Ignore the second so it cannot re-book a meeting or render a
	// duplicate menu after the step has already advanced.
	if msg.Type == "interactive" && msg.Interactive != nil {
		if rowID := interactiveID(msg.Interactive); rowID != "" {
			if first, err := h.store.ClaimInteraction(ctx, phone, rowID); err != nil {
				log.Warn("interactive dedup failed, processing anyway", "row_id", rowID, "err", err)
			} else if !first {
				log.Info("duplicate interactive selection ignored", "row_id", rowID)
				return
			}
		}
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
		_ = h.wa.SendText(ctx, phone, "🔄 सेशन रीसेट झाले. पुन्हा सुरू करण्यासाठी काहीही टाइप करा.")
		return
	}

	// The language picker was removed; Marathi is the only language. This also
	// upgrades in-flight sessions left in English/Hindi.
	if sess.Lang != defaultLang {
		sess.Lang = defaultLang
		if err := h.store.Save(ctx, sess); err != nil {
			log.Warn("save default language", "err", err)
		}
	}

	switch sess.Step {

	case store.StepStart, store.StepLangChosen, store.StepLeader:
		// New sessions, plus legacy sessions created before the language,
		// PIN-code and leader-selection steps were removed, go straight to
		// the preselected leader's Marathi menu.
		h.beginSession(ctx, phone, sess)

	case store.StepMainMenu:
		if msg.Type == "interactive" && msg.Interactive != nil {
			h.handleMenuSelection(ctx, phone, sess, msg.Interactive)
		} else {
			h.sendMenuList(ctx, phone, sess)
		}

	case store.StepComplaint:
		switch {
		case msg.Type == "text" && msg.Text != nil:
			h.handleComplaintDetails(ctx, phone, sess, msg.Text.Body)
		case msg.Type == "audio" && msg.Audio != nil:
			h.handleComplaintAudio(ctx, phone, sess, msg.Audio, msg.ContactName)
		default:
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

// beginSession binds the configured leader to the session and opens the
// e-Office main menu. The language, PIN-code and leader-selection steps are
// skipped; Marathi is always used.
func (h *Handler) beginSession(ctx context.Context, phone string, sess *store.Session) {
	ns, err := h.preselectedLeader(ctx)
	if err != nil {
		slog.Error("preselected leader lookup failed",
			"phone", phone,
			"leader_id", h.leaderID,
			"leader_slug", h.leaderSlug,
			"err", err,
		)
		// Leave the session in a retryable onboarding step so the next
		// message tries the leader lookup again.
		sess.Step = store.StepLangChosen
		if saveErr := h.store.Save(ctx, sess); saveErr != nil {
			slog.Error("save session", "phone", phone, "err", saveErr)
		}
		_ = h.wa.SendText(ctx, phone, h.t(sess).LeaderUnavailable)
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

	h.sendMainMenu(ctx, phone, sess, ns)
}

// preselectedLeader returns the leader configured for this deployment.
// LEADER_ID wins when set; otherwise the leader is resolved by LEADER_SLUG.
func (h *Handler) preselectedLeader(ctx context.Context) (*db.Nagarsevak, error) {
	if id := strings.TrimSpace(h.leaderID); id != "" {
		return h.repo.NagarsevakByID(ctx, id)
	}
	return h.repo.NagarsevakBySlug(ctx, strings.TrimSpace(h.leaderSlug))
}

// ─────────────────────────────────────────────
// Step 1 – main menu (10 options)
// ─────────────────────────────────────────────

func (h *Handler) sendMainMenu(ctx context.Context, phone string, sess *store.Session, ns *db.Nagarsevak) {
	t := h.t(sess)

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

// sendMenuList sends the main list: options 1–9 plus a "More Options" row.
// Meta caps one list message at 10 rows TOTAL, so the last option (NariShakti)
// is shown after tapping "More Options".
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

// sendMoreOptionsList sends option 10, shown after the user taps the
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

func (h *Handler) handleMenuSelection(ctx context.Context, phone string, sess *store.Session, ir *whatsapp.InteractiveReply) {
	id := listID(ir)

	if id == "" {
		// An interactive payload without a list row ID cannot be routed.
		// The menu is already on screen, so do not send another copy.
		return
	}

	if slotByID(id) != nil {
		// Slot rows belong to the meeting step. A slot row arriving while the
		// main menu is active is a stale/duplicate tap from a meeting that was
		// already booked; the booking flow already sent the menu, so sending
		// it again would duplicate it.
		slog.Info("stale slot selection ignored", "phone", phone, "row_id", id)
		return
	}

	if id == "opt_more" {
		// Reveal the remaining option(s) that do not fit in the 10-row list.
		h.sendMoreOptionsList(ctx, phone, sess)
		return
	}

	if !strings.HasPrefix(id, "opt_") {
		h.sendMenuList(ctx, phone, sess)
		return
	}

	var n int
	if _, err := fmt.Sscanf(id, "opt_%d", &n); err != nil || n < 1 || n > 10 {
		// Includes stale rows from the retired 11–13 options: simply redraw
		// the current menu.
		h.sendMenuList(ctx, phone, sess)
		return
	}
	h.dispatchOption(ctx, phone, sess, n)
}

// dispatchOption routes one of the 10 main-menu options.
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

	case 4, 5, 6, 7, 8, 9, 10:
		// Every informational option sends the same website-redirect message.
		h.sendInfoReply(ctx, phone, sess)
		h.sendMenuList(ctx, phone, sess)

	default:
		h.sendMenuList(ctx, phone, sess)
	}
}

// sendInfoReply sends the same website-redirect message for options 4–10,
// followed by the closing thank-you line.
func (h *Handler) sendInfoReply(ctx context.Context, phone string, sess *store.Session) {
	t := h.t(sess)
	_ = h.wa.SendText(ctx, phone, t.TransparencyText+"\n\n"+t.ThankYou)
}

// ─────────────────────────────────────────────
// Option 1 – raise complaint
// ─────────────────────────────────────────────

func (h *Handler) promptComplaint(ctx context.Context, phone string, sess *store.Session) {
	_ = h.wa.SendText(ctx, phone, h.t(sess).ComplaintPrompt)
}

// Voice complaints keep the audio as the complaint content. The API still
// requires a name and description, so the WhatsApp profile name is used when
// available and the description is a fixed placeholder for the office.
const (
	defaultCitizenName        = "Citizen"
	voiceComplaintDescription = "Voice complaint (audio attached)"
)

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

// handleComplaintAudio downloads a voice note, uploads it to S3 and files a
// grievance with the resulting public URL in audio_url.
func (h *Handler) handleComplaintAudio(
	ctx context.Context,
	phone string,
	sess *store.Session,
	audio *whatsapp.AudioMessage,
	contactName string,
) {
	t := h.t(sess)

	failAudio := func(stage string, err error) {
		slog.Error("voice complaint failed",
			"phone", phone, "stage", stage, "media_id", audio.ID, "err", err)
		_ = h.wa.SendText(ctx, phone, t.ComplaintAudioFailed+"\n\n"+t.ComplaintPrompt)
	}

	if h.audio == nil {
		failAudio("s3", fmt.Errorf("audio uploader is not configured"))
		return
	}
	if strings.TrimSpace(audio.ID) == "" {
		failAudio("download", fmt.Errorf("missing media id"))
		return
	}

	data, mimeType, err := h.wa.DownloadMedia(ctx, audio.ID)
	if err != nil {
		failAudio("download", err)
		return
	}
	mimeType = strings.TrimSpace(strings.Split(mimeType, ";")[0])
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	key := storage.AudioKey(phone, audio.ID, mimeType, time.Now())
	audioURL, err := h.audio.Upload(ctx, key, data, mimeType)
	if err != nil {
		failAudio("upload", err)
		return
	}

	name := strings.TrimSpace(contactName)
	if name == "" {
		name = defaultCitizenName
	}
	g, err := h.api.CreateGrievance(ctx, api.GrievanceRequest{
		NagarsevakID: sess.NagarsevakSlug,
		Name:         name,
		Mobile:       api.Mobile10(phone),
		Description:  voiceComplaintDescription,
		Ward:         sess.NagarsevakWard,
		AudioURL:     audioURL,
	})
	if err != nil {
		slog.Error("CreateGrievance (voice)", "phone", phone, "audio_url", audioURL, "err", err)
		_ = h.wa.SendText(ctx, phone, t.ComplaintFailed+"\n\n"+t.ComplaintPrompt)
		return
	}
	if g.TicketID == "" {
		slog.Error("CreateGrievance (voice): empty ticket ID", "phone", phone)
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
	rowID := listID(ir)

	slot := slotByID(rowID)
	if slot == nil {
		h.releaseInteraction(ctx, phone, rowID)
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
		h.releaseInteraction(ctx, phone, rowID)
		_ = h.wa.SendText(ctx, phone, t.MeetingSlotTaken)
		h.sendSlotList(ctx, phone, sess)
		return
	case err != nil:
		slog.Error("BookAppointment", "phone", phone, "date", sess.PendingDate, "time", slot[2], "err", err)
		h.releaseInteraction(ctx, phone, rowID)
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

// releaseInteraction clears the per-row dedup claim after a failure so the
// citizen can immediately retry the same list row.
func (h *Handler) releaseInteraction(ctx context.Context, phone, rowID string) {
	if err := h.store.ReleaseInteraction(ctx, phone, rowID); err != nil {
		slog.Warn("release interaction", "phone", phone, "row_id", rowID, "err", err)
	}
}

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
	return I18n[defaultLang]
}

// displayName picks the language-appropriate name for a leader.
func displayName(lang string, ns db.Nagarsevak) string {
	if (lang == "mr" || lang == "hi") && ns.NameHindi != "" {
		return ns.NameHindi
	}
	return ns.FullName
}

// menuRows builds the 10 main-menu rows in the session language.
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

// secondaryMenuRows returns the options revealed by "More Options": 10.
func secondaryMenuRows(t Strings) [][3]string {
	return menuRows(t)[9:]
}

func listID(ir *whatsapp.InteractiveReply) string {
	if ir == nil || ir.ListReply == nil {
		return ""
	}
	return ir.ListReply.ID
}

// interactiveID returns the selected row ID for either a button or a list
// reply. It is used for per-row deduplication, not for routing.
func interactiveID(ir *whatsapp.InteractiveReply) string {
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
