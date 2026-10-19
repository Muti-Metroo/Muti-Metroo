package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Muti-Metroo/Muti-Metroo/internal/filetransfer"
)

func TestWriteFileTransferError(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		handled bool
		status  int
		code    string
		msg     string
	}{
		{"plain error", errors.New("boom"), false, 0, "", ""},
		{"uncoded remote", &filetransfer.RemoteError{Message: "disk full"}, false, 0, "", ""},
		{"auth", &filetransfer.RemoteError{Code: filetransfer.ErrCodeAuthRequired, Message: "signature missing"}, true, http.StatusUnauthorized, "auth_required", "signature missing"},
		{"not found", fmt.Errorf("download: %w", &filetransfer.RemoteError{Code: filetransfer.ErrCodeNotFound, Message: "path not found: /x"}), true, http.StatusBadGateway, "not_found", "remote error: path not found: /x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if got := writeFileTransferError(rec, tc.err); got != tc.handled {
				t.Fatalf("handled = %v, want %v", got, tc.handled)
			}
			if !tc.handled {
				return
			}
			var body struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body: %v", err)
			}
			if rec.Code != tc.status || body.Code != tc.code || body.Error != tc.msg {
				t.Errorf("got %d %q %q, want %d %q %q", rec.Code, body.Code, body.Error, tc.status, tc.code, tc.msg)
			}
		})
	}
}
