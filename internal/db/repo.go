// Package db provides PostgreSQL access for the Ataljanseva backend.
// Connection pool is tuned for high concurrency via config values.
// The preselected leader is read from the political_users table; the citizen
// IVR upsert uses a pre-existing PostgreSQL procedure. Complaints and meetings
// live in the AtalJanseva REST API, not in this database.
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

// NagarsevakBySlug looks up the configured leader from political_users.
// The chatbot skips the PIN-code and leader-selection steps, so the leader is
// resolved directly by its unique slug.
func (r *Repo) NagarsevakBySlug(ctx context.Context, slug string) (*Nagarsevak, error) {
	start := time.Now()
	slog.Debug("NagarsevakBySlug", "slug", slug)

	row := r.db.QueryRowContext(ctx, `
		SELECT id::text,
		       full_name,
		       COALESCE(name_hindi, ''),
		       COALESCE(party, ''),
		       COALESCE(ward, ''),
		       slug,
		       COALESCE(profile_photo, '')
		FROM   political_users
		WHERE  slug = $1
		  AND  is_active = true
		LIMIT  1`,
		slug,
	)

	var n Nagarsevak
	if err := row.Scan(
		&n.ID, &n.FullName, &n.NameHindi,
		&n.Party, &n.Ward, &n.Slug,
		&n.ProfilePhoto,
	); err != nil {
		slog.Error("NagarsevakBySlug failed",
			"slug", slug,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		return nil, fmt.Errorf("NagarsevakBySlug: %w", err)
	}
	n.ProfilePhoto = strings.TrimSpace(n.ProfilePhoto)

	slog.Debug("NagarsevakBySlug success",
		"slug", slug,
		"name", n.FullName,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return &n, nil
}

// NagarsevakByID looks up a single leader by primary key.
// Reads political_users directly so nullable columns (ward, name_hindi, …)
// are normalised to empty strings before scanning.
func (r *Repo) NagarsevakByID(ctx context.Context, id string) (*Nagarsevak, error) {
	start := time.Now()
	slog.Debug("NagarsevakByID", "id", id)

	row := r.db.QueryRowContext(ctx, `
		SELECT id::text,
		       full_name,
		       COALESCE(name_hindi, ''),
		       COALESCE(party, ''),
		       COALESCE(ward, ''),
		       slug,
		       COALESCE(profile_photo, '')
		FROM   political_users
		WHERE  id::text = $1
		  AND  is_active = true
		LIMIT  1`,
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
