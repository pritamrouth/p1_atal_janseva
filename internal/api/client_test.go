package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestClient serves a fixed handler and returns a client pointed at it.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, 5*time.Second)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestCreateGrievance(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/grievances" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if in["nagarsevakId"] != "ashokshelke" {
			t.Errorf("nagarsevakId = %v", in["nagarsevakId"])
		}
		if in["mobile"] != "9876543210" {
			t.Errorf("mobile = %v", in["mobile"])
		}
		writeJSON(w, http.StatusCreated, `{
			"success": true,
			"message": "Grievance submitted",
			"data": {"id":"9d7c","ticketId":"TKT4821907","nagarsevakId":"ashokshelke",
			         "status":"Pending","assignedTo":"Sunita Jadhav"}
		}`)
	})

	g, err := c.CreateGrievance(context.Background(), GrievanceRequest{
		NagarsevakID: "ashokshelke",
		Name:         "Ramesh Patil",
		Mobile:       Mobile10("919876543210"),
		Description:  "No water supply since Monday",
		Address:      "Near Hanuman Mandir",
	})
	if err != nil {
		t.Fatalf("CreateGrievance: %v", err)
	}
	if g.TicketID != "TKT4821907" {
		t.Errorf("TicketID = %q", g.TicketID)
	}
	if g.Status != "Pending" {
		t.Errorf("Status = %q", g.Status)
	}
}

func TestCreateGrievanceRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"name and description are required"}`)
	})

	_, err := c.CreateGrievance(context.Background(), GrievanceRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsBadRequest(err) {
		t.Errorf("IsBadRequest = false, err = %v", err)
	}
	if !strings.Contains(err.Error(), "name and description are required") {
		t.Errorf("error = %v", err)
	}
}

func TestTrackTicket(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/grievances/track/ticket/TKT4821907" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, `{"success":true,"message":"Success",
			"data":{"ticketId":"TKT4821907","status":"Resolved","category":"Water"}}`)
	})

	g, err := c.TrackTicket(context.Background(), "TKT4821907")
	if err != nil {
		t.Fatalf("TrackTicket: %v", err)
	}
	if g.Status != "Resolved" || g.TicketID != "TKT4821907" {
		t.Errorf("got %+v", g)
	}
}

func TestTrackTicketNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound,
			`{"success":false,"message":"No grievance found with this ticket ID"}`)
	})

	_, err := c.TrackTicket(context.Background(), "TKT000000")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, err = %v", err)
	}
}

func TestBookedSlots(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("nagarsevakId") != "ashokshelke" || q.Get("date") != "2026-10-14" {
			t.Errorf("query = %v", q)
		}
		writeJSON(w, http.StatusOK, `{"success":true,"message":"Success","data":[
			{"time":"10:00:00","endTime":"10:30:00"},
			{"time":"15:00","endTime":"15:30"},
			{"endTime":"17:30"}
		]}`)
	})

	got, err := c.BookedSlots(context.Background(), "ashokshelke", "2026-10-14")
	if err != nil {
		t.Fatalf("BookedSlots: %v", err)
	}
	if !got["10:00"] || !got["15:00"] {
		t.Errorf("booked = %v", got)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestBookAppointmentConflict(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest,
			`{"success":false,"message":"This time slot is already booked. Please choose a different time."}`)
	})

	_, err := c.BookAppointment(context.Background(), BookRequest{
		NagarsevakID: "ashokshelke", Name: "Ramesh Patil",
		Phone: "9876543210", Date: "2026-10-14", Time: "10:00",
	})
	if !IsSlotTaken(err) {
		t.Fatalf("IsSlotTaken = false, err = %v", err)
	}
}

func TestBookAppointmentSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/schedule/appointments/book" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusCreated, `{"success":true,
			"message":"Appointment booked successfully",
			"data":{"id":"4be1c0d2","date":"2026-10-14","time":"10:00",
			        "endTime":"10:30","status":"Upcoming"}}`)
	})

	a, err := c.BookAppointment(context.Background(), BookRequest{
		NagarsevakID: "ashokshelke", Name: "Ramesh Patil",
		Phone: "9876543210", Date: "2026-10-14", Time: "10:00",
	})
	if err != nil {
		t.Fatalf("BookAppointment: %v", err)
	}
	if a.EndTime != "10:30" {
		t.Errorf("EndTime = %q", a.EndTime)
	}
}

func TestPublicListBareArray(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events/public" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "5" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		writeJSON(w, http.StatusOK,
			`{"success":true,"data":[{"title":"Diwali Milan","date":"2026-10-20"}]}`)
	})

	items, err := c.PublicList(context.Background(), "/api/events/public",
		url.Values{"limit": {"5"}})
	if err != nil {
		t.Fatalf("PublicList: %v", err)
	}
	if len(items) != 1 || FirstStr(items[0], "title") != "Diwali Milan" {
		t.Errorf("items = %v", items)
	}
}

func TestPublicListWrappedObject(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"success":true,"data":{
			"grievances":[{"ticketId":"TKT1","status":"Resolved"},
			               {"ticketId":"TKT2","status":"Resolved"}],
			"total":2,"page":1,"limit":50}}`)
	})

	items, err := c.PublicList(context.Background(), "/api/grievances/public/all", nil)
	if err != nil {
		t.Fatalf("PublicList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if FirstStr(items[0], "ticketId") != "TKT1" {
		t.Errorf("first item = %v", items[0])
	}
}

func TestPublicListEmpty(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"success":true,"data":{"grievances":[],"total":0}}`)
	})
	items, err := c.PublicList(context.Background(), "/api/work-reports/public", nil)
	if err != nil {
		t.Fatalf("PublicList: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items = %v, want empty", items)
	}
}

func TestProfileBySlug(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/political-users/slug/ashokshelke" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, `{"success":true,"data":{
			"user":{"name":"Ashok Shelke","designation":"Corporator",
			        "party":"BJP","photoUrl":"https://cdn/x.jpg"}}}`)
	})

	p, err := c.ProfileBySlug(context.Background(), "ashokshelke")
	if err != nil {
		t.Fatalf("ProfileBySlug: %v", err)
	}
	if FirstStr(p, "name") != "Ashok Shelke" {
		t.Errorf("profile = %v", p)
	}
	if FirstStr(p, "photoUrl") != "https://cdn/x.jpg" {
		t.Errorf("photo = %v", p)
	}
}

func TestItemsFromShapes(t *testing.T) {
	// Bare array.
	items := ItemsFrom(json.RawMessage(`[{"a":1}]`))
	if len(items) != 1 {
		t.Errorf("bare array → %d items", len(items))
	}
	// Unknown wrapper key still yields the array.
	items = ItemsFrom(json.RawMessage(`{"whatever":[{"a":1},{"b":2}]}`))
	if len(items) != 2 {
		t.Errorf("unknown wrapper → %d items", len(items))
	}
	// No array at all.
	if items := ItemsFrom(json.RawMessage(`{"total":0}`)); items != nil && len(items) != 0 {
		t.Errorf("no array → %v", items)
	}
	// Invalid JSON.
	if items := ItemsFrom(json.RawMessage(`nope`)); len(items) != 0 {
		t.Errorf("invalid → %v", items)
	}
}

func TestFirstStr(t *testing.T) {
	m := map[string]any{"ticketId": "TKT1", "ward": float64(12), "empty": "  ", "n": nil}
	if got := FirstStr(m, "missing", "ticketId"); got != "TKT1" {
		t.Errorf("FirstStr = %q", got)
	}
	if got := FirstStr(m, "ward"); got != "12" {
		t.Errorf("ward = %q", got)
	}
	if got := FirstStr(m, "empty", "missing"); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := FirstStr(m, "TicketId"); got != "TKT1" {
		t.Errorf("case-insensitive = %q", got)
	}
}

func TestSnippet(t *testing.T) {
	if got := Snippet("  a\n  b  ", 10); got != "a b" {
		t.Errorf("Snippet = %q", got)
	}
	if got := Snippet("abcdefghij", 4); got != "abcd…" {
		t.Errorf("truncated = %q", got)
	}
	if got := Snippet("अक्षर", 10); got != "अक्षर" {
		t.Errorf("devanagari = %q", got)
	}
}

func TestMobile10(t *testing.T) {
	cases := map[string]string{
		"9876543210":      "9876543210",
		"919876543210":    "9876543210",
		"+91 98765 43210": "9876543210",
		"09876543210":     "9876543210",
		"9198765432109":   "9198765432109", // 13 digits: not a 91-prefixed 12-digit number
		"":                "",
	}
	for in, want := range cases {
		if got := Mobile10(in); got != want {
			t.Errorf("Mobile10(%q) = %q, want %q", in, got, want)
		}
	}
}
