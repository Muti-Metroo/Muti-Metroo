package expose

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeDecodeFrame_RoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		frameType uint8
		streamID  uint64
		payload   []byte
	}{
		{"register", FrameRegister, 0, []byte(`{"forwards":[]}`)},
		{"register_ack", FrameRegisterAck, 0, []byte(`{"results":[]}`)},
		{"unregister", FrameUnregister, 0, []byte(`{"keys":["k1"]}`)},
		{"unregister_ack", FrameUnregisterAck, 0, []byte(`{"results":[]}`)},
		{"open", FrameOpen, 1, []byte(`{"key":"k","remote_addr":"1.2.3.4:80"}`)},
		{"open_ack", FrameOpenAck, 1, []byte(`{"ok":true}`)},
		{"data", FrameData, 42, []byte("hello world")},
		{"close_write", FrameCloseWrite, 7, nil},
		{"close", FrameClose, 7, nil},
		{"data_max_streamid", FrameData, 0xffff_ffff_ffff_ffff, []byte("a")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := EncodeFrame(tc.frameType, tc.streamID, tc.payload)
			ft, sid, body, err := DecodeFrame(buf)
			if err != nil {
				t.Fatalf("DecodeFrame: %v", err)
			}
			if ft != tc.frameType {
				t.Errorf("type = 0x%02x; want 0x%02x", ft, tc.frameType)
			}
			if sid != tc.streamID {
				t.Errorf("streamID = %d; want %d", sid, tc.streamID)
			}
			if !bytes.Equal(body, tc.payload) {
				t.Errorf("payload = %q; want %q", body, tc.payload)
			}
		})
	}
}

func TestDecodeFrame_ShortFrame(t *testing.T) {
	for _, n := range []int{0, 1, 8} {
		_, _, _, err := DecodeFrame(make([]byte, n))
		if !errors.Is(err, ErrShortFrame) {
			t.Errorf("len=%d: expected ErrShortFrame, got %v", n, err)
		}
	}
}

func TestDecodeFrame_UnknownType(t *testing.T) {
	buf := EncodeFrame(0xAB, 0, nil)
	_, _, _, err := DecodeFrame(buf)
	if !errors.Is(err, ErrUnknownFrameType) {
		t.Errorf("expected ErrUnknownFrameType, got %v", err)
	}
}

func TestEncodeDecodeNonceFrame(t *testing.T) {
	buf := EncodeNonceFrame(FramePing, 0xdeadbeefcafebabe)
	ft, sid, body, err := DecodeFrame(buf)
	if err != nil {
		t.Fatalf("DecodeFrame: %v", err)
	}
	if ft != FramePing {
		t.Errorf("type = 0x%02x; want FramePing", ft)
	}
	if sid != 0 {
		t.Errorf("streamID = %d; want 0", sid)
	}
	nonce, err := DecodeNonce(body)
	if err != nil {
		t.Fatalf("DecodeNonce: %v", err)
	}
	if nonce != 0xdeadbeefcafebabe {
		t.Errorf("nonce = %x; want deadbeefcafebabe", nonce)
	}
}

func TestDecodeNonce_BadLength(t *testing.T) {
	for _, n := range []int{0, 4, 7, 9, 16} {
		_, err := DecodeNonce(make([]byte, n))
		if err == nil {
			t.Errorf("len=%d: expected error", n)
		}
	}
}
