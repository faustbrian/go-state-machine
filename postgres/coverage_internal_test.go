package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	statemachine "github.com/faustbrian/golib/pkg/state-machine"
)

func TestResultCodecFailurePaths(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("codec failed")
	goodState := TextCodec[string]()
	goodEvent := TextCodec[string]()
	result := statemachine.Result[string, string]{
		DefinitionVersion: "v1", Previous: "a", Next: "b", Event: "go", TransitionID: "go",
	}
	tests := []struct {
		name  string
		store *Store[string, string]
	}{
		{"previous", &Store[string, string]{stateCodec: Codec[string]{Encode: func(value string) (string, error) {
			if value == "a" {
				return "", wantErr
			}
			return value, nil
		}}, eventCodec: goodEvent}},
		{"next", &Store[string, string]{stateCodec: Codec[string]{Encode: func(value string) (string, error) {
			if value == "b" {
				return "", wantErr
			}
			return value, nil
		}}, eventCodec: goodEvent}},
		{"event", &Store[string, string]{stateCodec: goodState, eventCodec: Codec[string]{Encode: func(string) (string, error) { return "", wantErr }}}},
	}
	for _, test := range tests {
		if _, _, err := test.store.encodeResult(result); !errors.Is(err, wantErr) {
			t.Fatalf("%s encode error = %v", test.name, err)
		}
	}
	store := &Store[string, string]{stateCodec: goodState, eventCodec: goodEvent}
	result.DefinitionVersion = ""
	if _, _, err := store.encodeResult(result); !errors.Is(err, statemachine.ErrInvalidStoreInput) {
		t.Fatalf("missing version error = %v", err)
	}
	result.DefinitionVersion = "v1"
	if _, _, err := store.encodeResult(result); err != nil {
		t.Fatalf("default marshal: %v", err)
	}
	failingStore := &Store[string, string]{
		stateCodec: Codec[string]{Encode: func(string) (string, error) { return "", wantErr }},
		eventCodec: goodEvent,
	}
	if _, _, err := failingStore.CompareAndTransition(t.Context(), "one", 0, result, time.Time{}); !errors.Is(err, wantErr) {
		t.Fatalf("transition codec error = %v", err)
	}
	if err := failingStore.SaveSnapshot(t.Context(), statemachine.Snapshot[string]{State: "a"}); !errors.Is(err, wantErr) {
		t.Fatalf("snapshot codec error = %v", err)
	}
}

func TestBoundedErrorTextPreservesUTF8(t *testing.T) {
	t.Parallel()

	text := boundedErrorText(errors.New(strings.Repeat("€", 2_000)))
	if len(text) > maxErrorBytes || !utf8.ValidString(text) {
		t.Fatalf("bounded error has %d bytes and valid=%t", len(text), utf8.ValidString(text))
	}
	if boundedErrorText(nil) != "" {
		t.Fatal("nil error produced text")
	}
	if boundedErrorText(errors.New("short")) != "short" {
		t.Fatal("short error changed")
	}
}

func TestResultDecodeFailurePaths(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("codec failed")
	goodState := TextCodec[string]()
	goodEvent := TextCodec[string]()
	encoded := []byte(`{"definition_version":"v1","previous":"a","next":"b","event":"go","transition_id":"go"}`)
	store := &Store[string, string]{stateCodec: goodState, eventCodec: goodEvent}
	if _, err := store.decodeResult([]byte(`{`)); err == nil {
		t.Fatal("malformed result decoded")
	}
	tests := []*Store[string, string]{
		{stateCodec: Codec[string]{Decode: func(value string) (string, error) {
			if value == "a" {
				return "", wantErr
			}
			return value, nil
		}}, eventCodec: goodEvent},
		{stateCodec: Codec[string]{Decode: func(value string) (string, error) {
			if value == "b" {
				return "", wantErr
			}
			return value, nil
		}}, eventCodec: goodEvent},
		{stateCodec: goodState, eventCodec: Codec[string]{Decode: func(string) (string, error) { return "", wantErr }}},
	}
	for _, failing := range tests {
		if _, err := failing.decodeResult(encoded); !errors.Is(err, wantErr) {
			t.Fatalf("decode error = %v", err)
		}
	}
	decoded, err := store.decodeResult(encoded)
	if err != nil || decoded.Next != "b" {
		t.Fatalf("decoded = %#v, %v", decoded, err)
	}
}
