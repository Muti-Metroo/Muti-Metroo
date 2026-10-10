package shell

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeMessage(t *testing.T) {
	tests := []struct {
		name    string
		msgType uint8
		payload []byte
	}{
		{
			name:    "empty payload",
			msgType: MsgStdin,
			payload: []byte{},
		},
		{
			name:    "stdin data",
			msgType: MsgStdin,
			payload: []byte("hello world"),
		},
		{
			name:    "stdout data",
			msgType: MsgStdout,
			payload: []byte("command output\n"),
		},
		{
			name:    "stderr data",
			msgType: MsgStderr,
			payload: []byte("error message\n"),
		},
		{
			name:    "binary data",
			msgType: MsgStdin,
			payload: []byte{0x00, 0xFF, 0x7F, 0x80},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeMessage(tt.msgType, tt.payload)

			msgType, decoded, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage() error = %v", err)
			}

			if msgType != tt.msgType {
				t.Errorf("DecodeMessage() msgType = %d, want %d", msgType, tt.msgType)
			}

			if string(decoded) != string(tt.payload) {
				t.Errorf("DecodeMessage() payload = %q, want %q", decoded, tt.payload)
			}
		})
	}
}

func TestDecodeMessage_Errors(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "nil data",
			data:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeMessage(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncodeDecodeMeta(t *testing.T) {
	meta := &ShellMeta{
		Command: "/bin/bash",
		Args:    []string{"-c", "echo hello"},
		Env:     map[string]string{"FOO": "bar"},
		WorkDir: "/tmp",
		TTY: &TTYSettings{
			Rows: 24,
			Cols: 80,
			Term: "xterm-256color",
		},
		Timeout: 60,
	}

	encoded, err := EncodeMeta(meta)
	if err != nil {
		t.Fatalf("EncodeMeta() error = %v", err)
	}

	// Decode and verify
	msgType, payload, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msgType != MsgMeta {
		t.Errorf("message type = %d, want %d", msgType, MsgMeta)
	}

	decoded, err := DecodeMeta(payload)
	if err != nil {
		t.Fatalf("DecodeMeta() error = %v", err)
	}

	if decoded.Command != meta.Command {
		t.Errorf("Command = %q, want %q", decoded.Command, meta.Command)
	}

	if len(decoded.Args) != len(meta.Args) {
		t.Errorf("Args length = %d, want %d", len(decoded.Args), len(meta.Args))
	}

	if decoded.TTY == nil {
		t.Error("TTY is nil, expected non-nil")
	} else {
		if decoded.TTY.Rows != meta.TTY.Rows {
			t.Errorf("TTY.Rows = %d, want %d", decoded.TTY.Rows, meta.TTY.Rows)
		}
		if decoded.TTY.Cols != meta.TTY.Cols {
			t.Errorf("TTY.Cols = %d, want %d", decoded.TTY.Cols, meta.TTY.Cols)
		}
	}
}

func TestEncodeDecodeAck(t *testing.T) {
	tests := []struct {
		name    string
		ack     *ShellAck
		wantErr bool
	}{
		{
			name:    "success",
			ack:     &ShellAck{Success: true},
			wantErr: false,
		},
		{
			name:    "failure with error",
			ack:     &ShellAck{Success: false, Error: "command not found"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeAck(tt.ack)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EncodeAck() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			msgType, payload, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage() error = %v", err)
			}

			if msgType != MsgAck {
				t.Errorf("message type = %d, want %d", msgType, MsgAck)
			}

			var decoded ShellAck
			if err := json.Unmarshal(payload, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if decoded.Success != tt.ack.Success {
				t.Errorf("Success = %v, want %v", decoded.Success, tt.ack.Success)
			}
			if decoded.Error != tt.ack.Error {
				t.Errorf("Error = %q, want %q", decoded.Error, tt.ack.Error)
			}
		})
	}
}

func TestEncodeDecodeResize(t *testing.T) {
	tests := []struct {
		name string
		rows uint16
		cols uint16
	}{
		{
			name: "standard terminal",
			rows: 24,
			cols: 80,
		},
		{
			name: "wide terminal",
			rows: 50,
			cols: 200,
		},
		{
			name: "minimum size",
			rows: 1,
			cols: 1,
		},
		{
			name: "max uint16",
			rows: 65535,
			cols: 65535,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeResize(tt.rows, tt.cols)

			msgType, payload, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage() error = %v", err)
			}

			if msgType != MsgResize {
				t.Errorf("message type = %d, want %d", msgType, MsgResize)
			}

			rows, cols, err := DecodeResize(payload)
			if err != nil {
				t.Fatalf("DecodeResize() error = %v", err)
			}

			if rows != tt.rows {
				t.Errorf("rows = %d, want %d", rows, tt.rows)
			}
			if cols != tt.cols {
				t.Errorf("cols = %d, want %d", cols, tt.cols)
			}
		})
	}
}

func TestDecodeResize_Errors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "empty",
			payload: []byte{},
		},
		{
			name:    "too short",
			payload: []byte{0x00, 0x18, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeResize(tt.payload)
			if err == nil {
				t.Error("DecodeResize() expected error, got nil")
			}
		})
	}
}

func TestEncodeDecodeSignal(t *testing.T) {
	tests := []struct {
		name   string
		signum uint8
	}{
		{name: "SIGINT", signum: 2},
		{name: "SIGTERM", signum: 15},
		{name: "SIGKILL", signum: 9},
		{name: "SIGHUP", signum: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeSignal(tt.signum)

			msgType, payload, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage() error = %v", err)
			}

			if msgType != MsgSignal {
				t.Errorf("message type = %d, want %d", msgType, MsgSignal)
			}

			signum, err := DecodeSignal(payload)
			if err != nil {
				t.Fatalf("DecodeSignal() error = %v", err)
			}

			if signum != tt.signum {
				t.Errorf("signum = %d, want %d", signum, tt.signum)
			}
		})
	}
}

func TestEncodeDecodeExit(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int32
	}{
		{name: "success", exitCode: 0},
		{name: "error", exitCode: 1},
		{name: "signal exit", exitCode: 128 + 9}, // SIGKILL
		{name: "negative", exitCode: -1},
		{name: "large positive", exitCode: 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeExit(tt.exitCode)

			msgType, payload, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage() error = %v", err)
			}

			if msgType != MsgExit {
				t.Errorf("message type = %d, want %d", msgType, MsgExit)
			}

			exitCode, err := DecodeExit(payload)
			if err != nil {
				t.Fatalf("DecodeExit() error = %v", err)
			}

			if exitCode != tt.exitCode {
				t.Errorf("exitCode = %d, want %d", exitCode, tt.exitCode)
			}
		})
	}
}

func TestEncodeDecodeError(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "simple error", message: "command not found"},
		{name: "empty error", message: ""},
		{name: "long error", message: "This is a very long error message that contains multiple lines and special characters: !@#$%^&*()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shellErr := &ShellError{Message: tt.message}
			encoded, err := EncodeError(shellErr)
			if err != nil {
				t.Fatalf("EncodeError() error = %v", err)
			}

			msgType, payload, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage() error = %v", err)
			}

			if msgType != MsgError {
				t.Errorf("message type = %d, want %d", msgType, MsgError)
			}

			var decoded ShellError
			if err := json.Unmarshal(payload, &decoded); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if decoded.Message != tt.message {
				t.Errorf("Message = %q, want %q", decoded.Message, tt.message)
			}
		})
	}
}

func TestMsgTypeName(t *testing.T) {
	tests := []struct {
		msgType  uint8
		expected string
	}{
		{MsgMeta, "META"},
		{MsgAck, "ACK"},
		{MsgStdin, "STDIN"},
		{MsgStdout, "STDOUT"},
		{MsgStderr, "STDERR"},
		{MsgResize, "RESIZE"},
		{MsgSignal, "SIGNAL"},
		{MsgExit, "EXIT"},
		{MsgError, "ERROR"},
		{0xFF, "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := MsgTypeName(tt.msgType)
			if got != tt.expected {
				t.Errorf("MsgTypeName(%d) = %q, want %q", tt.msgType, got, tt.expected)
			}
		})
	}
}

func TestEncodeStdinStdoutStderr(t *testing.T) {
	data := []byte("test data")

	// Test stdin
	stdinMsg := EncodeStdin(data)
	msgType, payload, err := DecodeMessage(stdinMsg)
	if err != nil {
		t.Fatalf("DecodeMessage(stdin) error = %v", err)
	}
	if msgType != MsgStdin {
		t.Errorf("stdin message type = %d, want %d", msgType, MsgStdin)
	}
	if string(payload) != string(data) {
		t.Errorf("stdin payload = %q, want %q", payload, data)
	}

	// Test stdout
	stdoutMsg := EncodeStdout(data)
	msgType, payload, err = DecodeMessage(stdoutMsg)
	if err != nil {
		t.Fatalf("DecodeMessage(stdout) error = %v", err)
	}
	if msgType != MsgStdout {
		t.Errorf("stdout message type = %d, want %d", msgType, MsgStdout)
	}
	if string(payload) != string(data) {
		t.Errorf("stdout payload = %q, want %q", payload, data)
	}

	// Test stderr
	stderrMsg := EncodeStderr(data)
	msgType, payload, err = DecodeMessage(stderrMsg)
	if err != nil {
		t.Fatalf("DecodeMessage(stderr) error = %v", err)
	}
	if msgType != MsgStderr {
		t.Errorf("stderr message type = %d, want %d", msgType, MsgStderr)
	}
	if string(payload) != string(data) {
		t.Errorf("stderr payload = %q, want %q", payload, data)
	}
}

func TestDecodeAck(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    *ShellAck
		wantErr bool
	}{
		{
			name:    "success ack",
			payload: []byte(`{"success":true}`),
			want:    &ShellAck{Success: true},
			wantErr: false,
		},
		{
			name:    "failure ack with error",
			payload: []byte(`{"success":false,"error":"command failed"}`),
			want:    &ShellAck{Success: false, Error: "command failed"},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			payload: []byte(`{invalid json`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty payload",
			payload: []byte{},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeAck(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeAck() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Success != tt.want.Success {
				t.Errorf("DecodeAck().Success = %v, want %v", got.Success, tt.want.Success)
			}
			if got.Error != tt.want.Error {
				t.Errorf("DecodeAck().Error = %q, want %q", got.Error, tt.want.Error)
			}
		})
	}
}

func TestDecodeError(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    *ShellError
		wantErr bool
	}{
		{
			name:    "simple error",
			payload: []byte(`{"message":"something went wrong"}`),
			want:    &ShellError{Message: "something went wrong"},
			wantErr: false,
		},
		{
			name:    "error with code",
			payload: []byte(`{"message":"error with code","code":42}`),
			want:    &ShellError{Message: "error with code", Code: 42},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			payload: []byte(`not valid json`),
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty payload",
			payload: []byte{},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeError(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeError() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Message != tt.want.Message {
				t.Errorf("DecodeError().Message = %q, want %q", got.Message, tt.want.Message)
			}
			if got.Code != tt.want.Code {
				t.Errorf("DecodeError().Code = %d, want %d", got.Code, tt.want.Code)
			}
		})
	}
}

func TestDecodeMeta_Errors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "valid meta",
			payload: []byte(`{"command":"echo","args":["hello"]}`),
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			payload: []byte(`{command: "invalid}`),
			wantErr: true,
		},
		{
			name:    "empty payload",
			payload: []byte{},
			wantErr: true,
		},
		{
			name:    "null payload",
			payload: []byte(`null`),
			wantErr: false, // JSON null decodes to zero value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeMeta(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeMeta() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeSignal_Errors(t *testing.T) {
	// Empty payload should error
	_, err := DecodeSignal([]byte{})
	if err == nil {
		t.Error("DecodeSignal() with empty payload should error")
	}
}

func TestDecodeExit_Errors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "valid exit code",
			payload: []byte{0x00, 0x00, 0x00, 0x00},
			wantErr: false,
		},
		{
			name:    "empty payload",
			payload: []byte{},
			wantErr: true,
		},
		{
			name:    "too short",
			payload: []byte{0x00, 0x00, 0x00},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeExit(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeExit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncodeMessage_TypeOnly(t *testing.T) {
	// Test encoding a message with just the type and no payload
	encoded := EncodeMessage(MsgMeta, nil)
	if len(encoded) != 1 {
		t.Errorf("EncodeMessage with nil payload should have length 1, got %d", len(encoded))
	}
	if encoded[0] != MsgMeta {
		t.Errorf("EncodeMessage type = %d, want %d", encoded[0], MsgMeta)
	}

	// Decoding should work and return empty payload
	msgType, payload, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}
	if msgType != MsgMeta {
		t.Errorf("DecodeMessage type = %d, want %d", msgType, MsgMeta)
	}
	if len(payload) != 0 {
		t.Errorf("DecodeMessage payload length = %d, want 0", len(payload))
	}
}

func TestEncodeMeta_MinimalFields(t *testing.T) {
	// Test encoding meta with only command
	meta := &ShellMeta{
		Command: "pwd",
	}

	encoded, err := EncodeMeta(meta)
	if err != nil {
		t.Fatalf("EncodeMeta() error = %v", err)
	}

	msgType, payload, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msgType != MsgMeta {
		t.Errorf("message type = %d, want %d", msgType, MsgMeta)
	}

	decoded, err := DecodeMeta(payload)
	if err != nil {
		t.Fatalf("DecodeMeta() error = %v", err)
	}

	if decoded.Command != meta.Command {
		t.Errorf("Command = %q, want %q", decoded.Command, meta.Command)
	}
	if decoded.TTY != nil {
		t.Error("TTY should be nil when not set")
	}
	if len(decoded.Args) != 0 {
		t.Errorf("Args should be empty, got %v", decoded.Args)
	}
}

func TestDecodeMessage_SingleByte(t *testing.T) {
	// A single byte message (just the type) should decode successfully
	data := []byte{MsgStdout}
	msgType, payload, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}
	if msgType != MsgStdout {
		t.Errorf("msgType = %d, want %d", msgType, MsgStdout)
	}
	if len(payload) != 0 {
		t.Errorf("payload length = %d, want 0", len(payload))
	}
}

func TestMsgTypeName_AllTypes(t *testing.T) {
	// Ensure all defined message types have proper names
	knownTypes := map[uint8]string{
		MsgMeta:       "META",
		MsgAck:        "ACK",
		MsgStdin:      "STDIN",
		MsgStdout:     "STDOUT",
		MsgStderr:     "STDERR",
		MsgResize:     "RESIZE",
		MsgSignal:     "SIGNAL",
		MsgExit:       "EXIT",
		MsgError:      "ERROR",
		MsgStdinClose: "STDIN_CLOSE",
	}

	for msgType, expectedName := range knownTypes {
		got := MsgTypeName(msgType)
		if got != expectedName {
			t.Errorf("MsgTypeName(%d) = %q, want %q", msgType, got, expectedName)
		}
	}

	// Test a few unknown types
	unknownTypes := []uint8{0, 100, 200, 255}
	for _, msgType := range unknownTypes {
		got := MsgTypeName(msgType)
		if got != "UNKNOWN" {
			t.Errorf("MsgTypeName(%d) = %q, want UNKNOWN", msgType, got)
		}
	}
}

func TestEncodeDecodeAck_RoundTrip(t *testing.T) {
	// Full round-trip test using DecodeAck function
	original := &ShellAck{Success: true, Error: ""}

	encoded, err := EncodeAck(original)
	if err != nil {
		t.Fatalf("EncodeAck() error = %v", err)
	}

	msgType, payload, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msgType != MsgAck {
		t.Errorf("message type = %d, want %d", msgType, MsgAck)
	}

	decoded, err := DecodeAck(payload)
	if err != nil {
		t.Fatalf("DecodeAck() error = %v", err)
	}

	if decoded.Success != original.Success {
		t.Errorf("Success = %v, want %v", decoded.Success, original.Success)
	}
	if decoded.Error != original.Error {
		t.Errorf("Error = %q, want %q", decoded.Error, original.Error)
	}
}

func TestEncodeDecodeError_RoundTrip(t *testing.T) {
	// Full round-trip test using DecodeError function
	original := &ShellError{Message: "test error", Code: 123}

	encoded, err := EncodeError(original)
	if err != nil {
		t.Fatalf("EncodeError() error = %v", err)
	}

	msgType, payload, err := DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msgType != MsgError {
		t.Errorf("message type = %d, want %d", msgType, MsgError)
	}

	decoded, err := DecodeError(payload)
	if err != nil {
		t.Fatalf("DecodeError() error = %v", err)
	}

	if decoded.Message != original.Message {
		t.Errorf("Message = %q, want %q", decoded.Message, original.Message)
	}
	if decoded.Code != original.Code {
		t.Errorf("Code = %d, want %d", decoded.Code, original.Code)
	}
}
