package database

import (
	"testing"
	"time"
)

func TestCursorEncodeDecode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := "0192f298-345e-7e38-af88-e43f851a819d"

	cursor := encodeCursor(now, id)
	if cursor == "" {
		t.Fatal("expected non-empty cursor")
	}

	decodedTime, decodedID, err := decodeCursor(cursor)
	if err != nil {
		t.Fatalf("unexpected error decoding cursor: %v", err)
	}

	if !decodedTime.Equal(now) {
		t.Errorf("expected time %v, got %v", now, decodedTime)
	}
	if decodedID != id {
		t.Errorf("expected id %s, got %s", id, decodedID)
	}
}

func TestCursorDecodeInvalid(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{
			name:   "invalid base64",
			cursor: "not-base64!@#",
		},
		{
			name:   "missing separator",
			cursor: "c29tZXN0cmluZw",
		},
		{
			name:   "invalid timestamp format",
			cursor: "aW52YWxpZC10aW1lfGlkLTEyMw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeCursor(tt.cursor)
			if err == nil {
				t.Error("expected error decoding invalid cursor, got nil")
			}
		})
	}
}
