package events

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validEvent = `{"schema_version":"autogit.event/1","event_class":"ingress","event_id":"01J7N6X8P5K2V4W6FQ8M9ABCDF","event_type":"session.idle","occurred_at":"2026-09-01T06:30:00Z","producer":{"kind":"adapter","adapter":"codex","version":"1.0.0","installation_id":"install","instance_id":"instance"},"scope":{"repo_id":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","session_id":"session"},"ordering":{"stream_id":"repo/session"},"idempotency":{"key":"idle-1","attempt":1},"payload":{}}`

func TestDecodeRejectsDuplicateKeysAndTrailingJSON(t *testing.T) {
	for name, input := range map[string]string{
		"duplicate": strings.Replace(validEvent, `"payload":{}`, `"payload":{},"payload":{}`, 1),
		"trailing":  validEvent + ` {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(input), 64<<10); err == nil || CodeOf(err) != "E_SCHEMA" {
				t.Fatalf("Decode() error = %v, want E_SCHEMA", err)
			}
		})
	}
}

func TestDecodeRejectsUnknownMajorAndInvalidProducerEventPair(t *testing.T) {
	unknown := strings.Replace(validEvent, `autogit.event/1`, `autogit.event/2`, 1)
	if _, err := Decode([]byte(unknown), 64<<10); err == nil || CodeOf(err) != "E_VERSION" {
		t.Fatalf("unknown major error = %v, want E_VERSION", err)
	}
	bad := strings.Replace(validEvent, `"event_class":"ingress"`, `"event_class":"domain"`, 1)
	if _, err := Decode([]byte(bad), 64<<10); err == nil || CodeOf(err) != "E_SCHEMA" {
		t.Fatalf("bad producer pair error = %v, want E_SCHEMA", err)
	}
}

func TestDecodeValidEventHasStableDigest(t *testing.T) {
	a, err := Decode([]byte(validEvent), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Decode([]byte(validEvent), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || !strings.HasPrefix(a.Digest, "sha256:") {
		t.Fatalf("digests differ or malformed: %q %q", a.Digest, b.Digest)
	}
}

func TestDecodeRejectsInvalidRepositoryDigest(t *testing.T) {
	input := strings.Replace(validEvent, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "repo", 1)
	if _, err := Decode([]byte(input), 64<<10); err == nil || CodeOf(err) != "E_SCHEMA" {
		t.Fatalf("invalid repo digest error = %v, want E_SCHEMA", err)
	}
}

func TestDecodeRejectsWrongScalarTypesAndEnums(t *testing.T) {
	bad := strings.Replace(validEvent, `"attempt":1`, `"attempt":"not-an-integer"`, 1)
	if _, err := Decode([]byte(bad), 64<<10); err == nil || CodeOf(err) != "E_SCHEMA" {
		t.Fatalf("wrong attempt accepted: %v", err)
	}
	bad = strings.Replace(validEvent, `"idempotency":{"key":"idle-1","attempt":1}`, `"idempotency":{"key":"idle-1","attempt":1},"capabilities":{"queue_state":"bogus","monotonic_sequence":"bad"}`, 1)
	if _, err := Decode([]byte(bad), 64<<10); err == nil || CodeOf(err) != "E_SCHEMA" {
		t.Fatalf("wrong capability accepted: %v", err)
	}
}

func TestGitSHAValidationAcceptsOnlyGitWidths(t *testing.T) {
	for _, width := range []int{40, 64} {
		if !gitSHARe.MatchString(strings.Repeat("a", width)) {
			t.Fatalf("width %d rejected", width)
		}
	}
	for _, width := range []int{39, 41, 63, 65} {
		if gitSHARe.MatchString(strings.Repeat("a", width)) {
			t.Fatalf("width %d accepted", width)
		}
	}
}

func TestDecodeAppliesEmbeddedSchemaTypes(t *testing.T) {
	bad := strings.Replace(validEvent, `"payload":{}`, `"payload":{"status":123}`, 1)
	if _, err := Decode([]byte(bad), 64<<10); err == nil || CodeOf(err) != "E_SCHEMA" {
		t.Fatalf("numeric payload status accepted: %v", err)
	}
}

func TestStoreMakesDuplicateDeliveryAndDigestConflictDurable(t *testing.T) {
	e, err := Decode([]byte(validEvent), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.Accept(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if first.Disposition != Accepted {
		t.Fatalf("first = %+v", first)
	}
	dup, err := s.Accept(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if dup.Disposition != Duplicate {
		t.Fatalf("duplicate = %+v", dup)
	}
	changed := e
	changed.Digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err := s.Accept(context.Background(), changed); err == nil || CodeOf(err) != "E_IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestStoreBuffersMissingCausation(t *testing.T) {
	raw := strings.Replace(validEvent, `"causation_id"`, `"causation_id"`, 1)
	raw = strings.Replace(raw, `"ordering":{"stream_id":"repo/session"}`, `"ordering":{"stream_id":"repo/session","causation_id":"01J7N6X8P5K2V4W6FQ8M9ABCD0"}`, 1)
	e, err := Decode([]byte(raw), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Accept(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if r.Disposition != Pending {
		t.Fatalf("receipt = %+v, want pending", r)
	}
}

func TestStoreUsesRestrictivePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("state mode = %o, want 600", st.Mode().Perm())
	}
}

func TestStoreReplaysCausalPendingAfterPredecessorArrives(t *testing.T) {
	raw := strings.Replace(validEvent, `"ordering":{"stream_id":"repo/session"}`, `"ordering":{"stream_id":"repo/session","causation_id":"01J7N6X8P5K2V4W6FQ8M9ABCD0"}`, 1)
	e, err := Decode([]byte(raw), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Accept(context.Background(), e); err != nil || got.Disposition != Pending {
		t.Fatalf("pending=%+v err=%v", got, err)
	}
	n, err := s.ReplayPending(context.Background(), "01J7N6X8P5K2V4W6FQ8M9ABCD0")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("replayed=%d", n)
	}
}

func TestPendingBufferStoresBoundedFactsInsteadOfRawPayload(t *testing.T) {
	raw := strings.Replace(validEvent, `"ordering":{"stream_id":"repo/session"}`, `"ordering":{"stream_id":"repo/session","causation_id":"01J7N6X8P5K2V4W6FQ8M9ABCD0"}`, 1)
	raw = strings.Replace(raw, `"payload":{}`, `"payload":{"reason":"secret/source/path","extensions":{"raw":"secret-token"}}`, 1)
	e, err := Decode([]byte(raw), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Accept(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := s.db.QueryRow(`SELECT payload FROM pending_events WHERE event_id=?`, e.EventID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "secret/source/path") || strings.Contains(string(payload), "secret-token") {
		t.Fatalf("pending buffer retained raw payload: %s", payload)
	}
}

func TestOpenStoreMigratesLegacyAuditAndLogsValidateRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE audit_events (revision INTEGER PRIMARY KEY AUTOINCREMENT, reason_code TEXT NOT NULL, metadata TEXT NOT NULL, created_at TEXT NOT NULL)`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.db.Query(`PRAGMA table_info(audit_events)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"repository_id", "disposition", "reason_code"} {
		if !columns[name] {
			t.Fatalf("legacy audit missing migrated column %q", name)
		}
	}
}

func TestLogsRejectsUnstableReasonAndNonUTCRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repoID := "hmac-sha256:" + strings.Repeat("a", 64)
	if _, err := s.db.Exec(`INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES(?,?,?,?,?)`, repoID, "accepted", "secret-reason", `{"digest":"sha256:`+strings.Repeat("b", 64)+`"}`, "2026-09-01T06:30:00+00:00"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Logs(context.Background(), repoID, 1); err == nil || CodeOf(err) != "E_STATE" {
		t.Fatalf("Logs error=%v, want E_STATE", err)
	}
}

func TestLogsRejectsAuditMetadataWithExtraFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repoID := "hmac-sha256:" + strings.Repeat("a", 64)
	if _, err := s.db.Exec(`INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES(?,?,?,?,?)`, repoID, "accepted", "ACCEPTED", `{"digest":"sha256:`+strings.Repeat("b", 64)+`","extra":"sentinel"}`, "2026-09-01T06:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Logs(context.Background(), repoID, 1); err == nil || CodeOf(err) != "E_STATE" {
		t.Fatalf("Logs error=%v, want E_STATE", err)
	}
}

func TestLogsRejectsDigestThatDoesNotMatchReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repoID := "hmac-sha256:" + strings.Repeat("a", 64)
	good := "sha256:" + strings.Repeat("a", 64)
	bad := "sha256:" + strings.Repeat("b", 64)
	if _, err := s.db.Exec(`INSERT INTO event_receipts(event_id,idempotency_key,payload_digest,disposition,revision,created_at) VALUES(?,?,?,?,?,?)`, "event", "key", good, "accepted", 1, "2026-09-01T06:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES(?,?,?,?,?)`, repoID, "accepted", "ACCEPTED", `{"digest":"`+bad+`"}`, "2026-09-01T06:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Logs(context.Background(), repoID, 1); err == nil || CodeOf(err) != "E_STATE" {
		t.Fatalf("Logs error=%v, want E_STATE", err)
	}
}

func TestLogsRejectsDuplicateAuditMetadataKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	repoID := "hmac-sha256:" + strings.Repeat("a", 64)
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, err := s.db.Exec(`INSERT INTO event_receipts(event_id,idempotency_key,payload_digest,disposition,revision,created_at) VALUES(?,?,?,?,?,?)`, "event", "key", digest, "accepted", 1, "2026-09-01T06:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO audit_events(repository_id,disposition,reason_code,metadata,created_at) VALUES(?,?,?,?,?)`, repoID, "accepted", "ACCEPTED", `{"digest":"`+digest+`","digest":"`+digest+`"}`, "2026-09-01T06:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Logs(context.Background(), repoID, 1); err == nil || CodeOf(err) != "E_STATE" {
		t.Fatalf("Logs error=%v, want E_STATE", err)
	}
}

func TestAcceptAndProjectRollsBackReceiptAndProjectionOnProjectorFailure(t *testing.T) {
	e, err := Decode([]byte(validEvent), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	failed := errors.New("injected projector failure")
	if _, err := s.AcceptAndProject(context.Background(), e, func([]byte, Event) (ProjectionResult, error) {
		return ProjectionResult{}, failed
	}); !errors.Is(err, failed) {
		t.Fatalf("projector error=%v", err)
	}
	if _, _, err := s.LifecycleProjection(stringValue(e.Scope["repo_id"])); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("projection after rollback err=%v", err)
	}
	if _, err := s.AcceptAndProject(context.Background(), e, func([]byte, Event) (ProjectionResult, error) {
		return ProjectionResult{Data: []byte(`{"revision":1}`), Disposition: Accepted, Revision: 1}, nil
	}); err != nil {
		t.Fatalf("retry after rollback=%v", err)
	}
}
