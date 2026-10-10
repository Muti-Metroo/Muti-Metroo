package health

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/postalsys/muti-metroo/internal/filetransfer"
	"github.com/postalsys/muti-metroo/internal/identity"
)

// TestHandleFileUpload_AuthErrors asserts that the upload HTTP handler maps
// filetransfer.RemoteError auth codes to the matching HTTP status with a
// JSON body carrying the stable code. This is the contract the Muti Metroo Manager
// frontend relies on to decide when to prompt for a password.
func TestHandleFileUpload_AuthErrors(t *testing.T) {
	localID, _ := identity.NewAgentID()
	targetID, _ := identity.NewAgentID()

	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "upload.txt")
	if err := os.WriteFile(localPath, []byte("hello"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	tests := []struct {
		name       string
		remoteErr  error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "auth required",
			remoteErr:  &filetransfer.RemoteError{Code: filetransfer.ErrCodeAuthRequired, Message: "authentication required"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   filetransfer.ErrCodeAuthRequired,
		},
		{
			name:       "invalid credentials",
			remoteErr:  &filetransfer.RemoteError{Code: filetransfer.ErrCodeInvalidCredentials, Message: "invalid credentials"},
			wantStatus: http.StatusForbidden,
			wantCode:   filetransfer.ErrCodeInvalidCredentials,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			s := NewServer(cfg, &mockStatsProvider{running: true})
			remoteErr := tc.remoteErr
			s.SetRemoteProvider(&mockRemoteStatusProvider{
				id: localID,
				uploadFileFn: func(ctx context.Context, _ identity.AgentID, _, _ string, _ TransferOptions, _ FileTransferProgress) error {
					return remoteErr
				},
			})

			body, contentType := buildUploadForm(t, localPath, "/tmp/out.txt")
			req := httptest.NewRequest(http.MethodPost, "/agents/"+targetID.String()+"/file/upload", body)
			req.Header.Set("Content-Type", contentType)
			rec := httptest.NewRecorder()

			s.server.Handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v (body: %s)", err, rec.Body.String())
			}
			if code, _ := payload["code"].(string); code != tc.wantCode {
				t.Errorf("code = %q, want %q (body: %s)", code, tc.wantCode, rec.Body.String())
			}
			if _, ok := payload["error"].(string); !ok {
				t.Errorf("expected error field with string value, body: %s", rec.Body.String())
			}
		})
	}
}

// TestHandleFileDownload_AuthErrors mirrors the upload test for download.
func TestHandleFileDownload_AuthErrors(t *testing.T) {
	localID, _ := identity.NewAgentID()
	targetID, _ := identity.NewAgentID()

	tests := []struct {
		name       string
		remoteErr  error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "auth required",
			remoteErr:  &filetransfer.RemoteError{Code: filetransfer.ErrCodeAuthRequired, Message: "authentication required"},
			wantStatus: http.StatusUnauthorized,
			wantCode:   filetransfer.ErrCodeAuthRequired,
		},
		{
			name:       "invalid credentials",
			remoteErr:  &filetransfer.RemoteError{Code: filetransfer.ErrCodeInvalidCredentials, Message: "invalid credentials"},
			wantStatus: http.StatusForbidden,
			wantCode:   filetransfer.ErrCodeInvalidCredentials,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultServerConfig()
			s := NewServer(cfg, &mockStatsProvider{running: true})
			remoteErr := tc.remoteErr
			s.SetRemoteProvider(&mockRemoteStatusProvider{
				id: localID,
				downloadFileStreamFn: func(ctx context.Context, _ identity.AgentID, _ string, _ TransferOptions) (*DownloadStreamResult, error) {
					return nil, remoteErr
				},
			})

			reqBody, _ := json.Marshal(map[string]string{"path": "/tmp/out.txt"})
			req := httptest.NewRequest(http.MethodPost, "/agents/"+targetID.String()+"/file/download", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			s.server.Handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if code, _ := payload["code"].(string); code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// buildUploadForm constructs a multipart request body for file upload.
func buildUploadForm(t *testing.T, localPath, remotePath string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	fw, err := writer.CreateFormFile("file", filepath.Base(localPath))
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	f, err := os.Open(localPath)
	if err != nil {
		t.Fatalf("open local file: %v", err)
	}
	defer f.Close()
	if _, err := io.Copy(fw, f); err != nil {
		t.Fatalf("copy file: %v", err)
	}

	if err := writer.WriteField("path", remotePath); err != nil {
		t.Fatalf("write path field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, writer.FormDataContentType()
}
