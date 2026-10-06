// Package db provides PostgreSQL access for the Ataljanseva backend.
// Connection pool is tuned for high concurrency via config values.
// Leader (nagarsevak) reads go through pre-existing PostgreSQL functions;
// complaints and meetings use the tables created by our own migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// ─────────────────────────────────────────────
// Domain types
// ─────────────────────────────────────────────

// LocationInfo holds the geographic metadata returned alongside leaders.
type LocationInfo struct {
	State         string
	District      string
	StateHindi    string
	DistrictHindi string
}

// Nagarsevak holds a single leader's details.
// ProfilePhoto is a public URL stored in the profile_photo column.
type Nagarsevak struct {
	ID           string
	FullName     string
	NameHindi    string
	Party        string
	Ward         string
	Slug         string
	ProfilePhoto string
}

// NagarsevakResult bundles location metadata and the matched leaders
// returned from a single DB round-trip.
type NagarsevakResult struct {
	Location LocationInfo
	List     []Nagarsevak
}

// Complaint is a grievance raised through the chatbot.
type Complaint struct {
	TicketID    string
	Phone       string
	Lang        string
	FullName    string
	Description string
	Address     string
	LeaderID    string
	Pincode     string
	Status      string // in_progress | resolved
}

// Meeting is an appointment scheduled through the chatbot.
type Meeting struct {
	Phone         string
	Lang          string
	FullName      string
	PreferredDate string // ISO date (YYYY-MM-DD)
	Reason        string
	Slot          string
	LeaderID      string
	Pincode       string
}

// ─────────────────────────────────────────────
// Repository
// ─────────────────────────────────────────────

// Repo wraps a *sql.DB connection pool.
type Repo struct {
	db *sql.DB
}

// New opens a PostgreSQL connection pool with tuned settings and verifies
// connectivity before returning.
func New(dsn string, maxOpen, maxIdle int) (*Repo, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("db.Ping: %w", err)
	}

	return &Repo{db: db}, nil
}

// Close releases all resources held by the connection pool.
func (r *Repo) Close() error { return r.db.Close() }

// ─────────────────────────────────────────────
// Leaders (nagarsevaks)
// ─────────────────────────────────────────────

// NagarsevaksByPincode fetches all active leaders for a given pincode.
// Calls PostgreSQL function: get_nagarsevaks_by_pincode(TEXT)
func (r *Repo) NagarsevaksByPincode(ctx context.Context, pincode string) (*NagarsevakResult, error) {
	start := time.Now()
	slog.Debug("NagarsevaksByPincode", "pincode", pincode)

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT * FROM get_nagarsevaks_by_pincode($1)`,
		pincode,
	)
	if err != nil {
		slog.Error("NagarsevaksByPincode query failed",
			"pincode", pincode,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return nil, fmt.Errorf("NagarsevaksByPincode: %w", err)
	}
	defer rows.Close()

	var (
		result  *NagarsevakResult
		seenIDs = make(map[string]struct{})
	)

	for rows.Next() {
		var (
			state, district, stateHindi, districtHindi           string
			id, fullName, nameHindi, party, ward, slug, photoURL string
		)

		if err := rows.Scan(
			&state, &district, &stateHindi, &districtHindi,
			&id, &fullName, &nameHindi, &party,
			&ward, &slug, &photoURL,
		); err != nil {
			slog.Error("NagarsevaksByPincode scan failed",
				"pincode", pincode,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", err,
			)
			return nil, fmt.Errorf("NagarsevaksByPincode scan: %w", err)
		}

		// Initialise result from the first row.
		// Location columns are identical across all rows for a given pincode.
		if result == nil {
			result = &NagarsevakResult{
				Location: LocationInfo{
					State:         state,
					District:      district,
					StateHindi:    stateHindi,
					DistrictHindi: districtHindi,
				},
			}
		}

		// Deduplicate by ID.
		if _, seen := seenIDs[id]; !seen {
			seenIDs[id] = struct{}{}
			result.List = append(result.List, Nagarsevak{
				ID:           id,
				FullName:     fullName,
				NameHindi:    nameHindi,
				Party:        party,
				Ward:         ward,
				Slug:         slug,
				ProfilePhoto: strings.TrimSpace(photoURL),
			})
		}
	}

	if err := rows.Err(); err != nil {
		slog.Error("NagarsevaksByPincode rows error",
			"pincode", pincode,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return nil, fmt.Errorf("NagarsevaksByPincode rows: %w", err)
	}

	if result == nil {
		return nil, sql.ErrNoRows
	}

	slog.Debug("NagarsevaksByPincode success",
		"pincode", pincode,
		"leaders", len(result.List),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return result, nil
}

// NagarsevakByID looks up a single leader by primary key.
// Calls PostgreSQL function: get_nagarsevak_by_id(TEXT)
func (r *Repo) NagarsevakByID(ctx context.Context, id string) (*Nagarsevak, error) {
	start := time.Now()
	slog.Debug("NagarsevakByID", "id", id)

	row := r.db.QueryRowContext(
		ctx,
		`SELECT * FROM get_nagarsevak_by_id($1)`,
		id,
	)

	var n Nagarsevak
	if err := row.Scan(
		&n.ID, &n.FullName, &n.NameHindi,
		&n.Party, &n.Ward, &n.Slug,
		&n.ProfilePhoto,
	); err != nil {
		slog.Error("NagarsevakByID failed",
			"id", id,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return nil, fmt.Errorf("NagarsevakByID: %w", err)
	}
	n.ProfilePhoto = strings.TrimSpace(n.ProfilePhoto)

	slog.Debug("NagarsevakByID success",
		"id", id,
		"name", n.FullName,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return &n, nil
}

// UpsertCitizenIVR persists a citizen's completed session to the database.
// Called when the inactivity monitor fires (session expires after 90 s silence).
// Calls PostgreSQL procedure: upsert_citizen_ivr(phone, lang, pincode, ward, nagarsevak_id)
func (r *Repo) UpsertCitizenIVR(
	ctx context.Context,
	phone string,
	lang string,
	pincode string,
	ward string,
	nagarsevakID string,
) error {
	start := time.Now()
	slog.Debug("UpsertCitizenIVR",
		"phone", phone,
		"lang", lang,
		"pincode", pincode,
		"ward", ward,
		"nagarsevak_id", nagarsevakID,
	)

	_, err := r.db.ExecContext(
		ctx,
		`CALL upsert_citizen_ivr($1, $2, $3, $4, $5)`,
		phone,
		lang,
		pincode,
		ward,
		nagarsevakID,
	)
	if err != nil {
		slog.Error("UpsertCitizenIVR failed",
			"phone", phone,
			"lang", lang,
			"pincode", pincode,
			"ward", ward,
			"nagarsevak_id", nagarsevakID,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return fmt.Errorf("UpsertCitizenIVR: %w", err)
	}

	slog.Info("UpsertCitizenIVR success",
		"phone", phone,
		"lang", lang,
		"pincode", pincode,
		"ward", ward,
		"nagarsevak_id", nagarsevakID,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

// ─────────────────────────────────────────────
// Complaints
// ─────────────────────────────────────────────

// CreateComplaint inserts a complaint and returns its generated ticket ID
// (e.g. "AJ100001") from the complaint_ticket_seq sequence.
func (r *Repo) CreateComplaint(ctx context.Context, c *Complaint) (string, error) {
	start := time.Now()

	var ticketID string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO complaints
			(ticket_id, phone, lang, full_name, description, address, leader_id, pincode)
		VALUES
			('AJ' || nextval('complaint_ticket_seq')::text, $1, $2, $3, $4, $5, $6, $7)
		RETURNING ticket_id`,
		c.Phone, c.Lang, c.FullName, c.Description, c.Address, c.LeaderID, c.Pincode,
	).Scan(&ticketID)
	if err != nil {
		slog.Error("CreateComplaint failed",
			"phone", c.Phone,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return "", fmt.Errorf("CreateComplaint: %w", err)
	}

	slog.Info("complaint created",
		"ticket_id", ticketID,
		"phone", c.Phone,
		"leader_id", c.LeaderID,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return ticketID, nil
}

// GetComplaintStatus returns the status of a ticket ("in_progress" | "resolved").
// Returns sql.ErrNoRows when the ticket ID does not exist.
func (r *Repo) GetComplaintStatus(ctx context.Context, ticketID string) (string, error) {
	start := time.Now()

	var status string
	err := r.db.QueryRowContext(ctx,
		`SELECT status FROM complaints WHERE ticket_id = $1`,
		ticketID,
	).Scan(&status)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Error("GetComplaintStatus failed",
				"ticket_id", ticketID,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", err,
			)
			return "", fmt.Errorf("GetComplaintStatus: %w", err)
		}
		return "", sql.ErrNoRows
	}

	slog.Debug("GetComplaintStatus success",
		"ticket_id", ticketID,
		"status", status,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return status, nil
}

// ─────────────────────────────────────────────
// Meetings
// ─────────────────────────────────────────────

// CreateMeeting inserts a scheduled appointment.
func (r *Repo) CreateMeeting(ctx context.Context, m *Meeting) error {
	start := time.Now()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO meetings
			(phone, lang, full_name, preferred_date, reason, slot, leader_id, pincode)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		m.Phone, m.Lang, m.FullName, m.PreferredDate, m.Reason, m.Slot, m.LeaderID, m.Pincode,
	)
	if err != nil {
		slog.Error("CreateMeeting failed",
			"phone", m.Phone,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return fmt.Errorf("CreateMeeting: %w", err)
	}

	slog.Info("meeting scheduled",
		"phone", m.Phone,
		"name", m.FullName,
		"date", m.PreferredDate,
		"slot", m.Slot,
		"leader_id", m.LeaderID,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}
