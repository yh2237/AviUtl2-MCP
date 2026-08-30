package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	want := []byte(`{"method":"ping","日本語":true}`)
	var stream bytes.Buffer
	if err := WriteFrame(&stream, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	header := []byte{1, 0, 64, 0}
	_, err := ReadFrame(bytes.NewReader(header))
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("got %v, want ErrMessageTooLarge", err)
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{4, 0, 0, 0, 'p', 'i', 'n', 'g'})
	f.Add([]byte{255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadFrame(bytes.NewReader(data))
	})
}
