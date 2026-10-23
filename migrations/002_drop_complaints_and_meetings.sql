-- Complaints and appointments now live in the AtalJanseva REST API
-- (POST /api/grievances, POST /api/schedule/appointments/book); the chatbot's
-- local copies are no longer used. Drops them where an earlier deployment
-- created them. Idempotent: safe to run more than once.

DROP TABLE IF EXISTS complaints;
DROP SEQUENCE IF EXISTS complaint_ticket_seq;
DROP TABLE IF EXISTS meetings;
