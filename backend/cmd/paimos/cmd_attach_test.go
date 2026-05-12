package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachCommandUploadsLinksAndFetchesMetadata(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "screenshot.txt")
	if err := os.WriteFile(filePath, []byte("small file"), 0644); err != nil {
		t.Fatal(err)
	}

	seenUpload := false
	seenLink := false
	seenMeta := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-1":
			_, _ = w.Write([]byte(`{"id":101,"issue_key":"PAI-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/attachments":
			seenUpload = true
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			f, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("missing file field: %v", err)
			} else {
				_ = f.Close()
				if header.Filename != "screenshot.txt" {
					t.Errorf("filename=%q", header.Filename)
				}
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":7,"issue_id":0,"filename":"screenshot.txt","content_type":"text/plain","size_bytes":10}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/attachments/link":
			seenLink = true
			var body struct {
				IssueID       int64   `json:"issue_id"`
				AttachmentIDs []int64 `json:"attachment_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode link: %v", err)
			}
			if body.IssueID != 101 || len(body.AttachmentIDs) != 1 || body.AttachmentIDs[0] != 7 {
				t.Errorf("link body=%+v", body)
			}
			_, _ = w.Write([]byte(`{"linked":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/attachments/7/meta":
			seenMeta = true
			_, _ = w.Write([]byte(`{"id":7,"issue_id":101,"filename":"screenshot.txt","content_type":"text/plain","size_bytes":10}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	out, _, err := executeCLIForTest(t, "--json", "attach", "PAI-1", filePath)
	if err != nil {
		t.Fatalf("executeCLIForTest: %v", err)
	}
	if !seenUpload || !seenLink || !seenMeta {
		t.Fatalf("seen upload/link/meta = %v/%v/%v", seenUpload, seenLink, seenMeta)
	}
	if !strings.Contains(out, `"issue_id": 101`) || !strings.Contains(out, `"filename": "screenshot.txt"`) {
		t.Fatalf("stdout missing attachment metadata: %s", out)
	}
}

func TestAttachCommandRollsBackPendingUploadWhenLinkFails(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(filePath, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-2":
			_, _ = w.Write([]byte(`{"id":102}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/attachments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":8,"filename":"artifact.txt"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/attachments/link":
			http.Error(w, `{"error":"link failed"}`, http.StatusInternalServerError)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/attachments/8":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "attach", "PAI-2", filePath)
	if err == nil {
		t.Fatal("expected link failure")
	}
	if !deleted {
		t.Fatal("pending attachment was not rolled back")
	}
}

func TestAttachCommandSurfacesUploadSizeErrorsWithoutRollback(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "too-big.bin")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	deleteCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/PAI-3":
			_, _ = w.Write([]byte(`{"id":103}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/attachments":
			http.Error(w, `{"error":"file too large (max 10 MB)"}`, http.StatusRequestEntityTooLarge)
		case r.Method == http.MethodDelete:
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv(envURL, srv.URL)
	t.Setenv(envAPIKey, "test_key")

	_, _, err := executeCLIForTest(t, "--json", "attach", "PAI-3", filePath)
	if err == nil {
		t.Fatal("expected upload size failure")
	}
	if deleteCalls != 0 {
		t.Fatalf("deleteCalls=%d, want 0", deleteCalls)
	}
}
