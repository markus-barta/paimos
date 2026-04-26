package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalState_SortsKeysAndCapsStrings(t *testing.T) {
	long := strings.Repeat("x", snapshotStringCap+128)
	blob, hash, err := canonicalState(map[string]any{
		"z": 1,
		"a": long,
	})
	if err != nil {
		t.Fatalf("canonicalState: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("unmarshal canonical state: %v", err)
	}
	capped, ok := decoded["a"].(string)
	if !ok {
		t.Fatalf("expected capped string field")
	}
	if got := len(capped); got != snapshotStringCap {
		t.Fatalf("expected capped string length %d, got %d", snapshotStringCap, got)
	}
	if idxA := strings.Index(string(blob), `"a"`); idxA == -1 {
		t.Fatalf("expected canonical JSON to contain key a")
	} else if idxZ := strings.Index(string(blob), `"z"`); idxZ == -1 || idxA > idxZ {
		t.Fatalf("expected canonical JSON keys to be sorted: %s", string(blob))
	}

	sum := sha256.Sum256(blob)
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected canonical hash")
	}
}

func TestRecordMutation_RejectsOutsideTransaction(t *testing.T) {
	if _, err := recordMutation(t.Context(), nil, mutationRecordArgs{}); err == nil {
		t.Fatalf("expected recordMutation to require a transaction")
	}
}

func TestSanitizeSnapshotValue_DoesNotInventPromptBodyFields(t *testing.T) {
	sentinel := "LLM_RESPONSE_SENTINEL_SHOULD_NOT_BE_WIDENED"
	sanitized := sanitizeSnapshotValue(map[string]any{
		"title":               "Issue title",
		"description":         sentinel,
		"acceptance_criteria": sentinel,
		"notes":               sentinel,
		"provider_payload": map[string]any{
			"body": strings.Repeat("b", snapshotStringCap+256),
		},
	})
	blob, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatalf("marshal sanitized state: %v", err)
	}
	if !strings.Contains(string(blob), sentinel) {
		t.Fatalf("expected entity fields to remain present in snapshots")
	}
	if strings.Contains(string(blob), strings.Repeat("b", snapshotStringCap+64)) {
		t.Fatalf("expected nested body-like fields to be capped")
	}
}
