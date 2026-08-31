package jsonc

import (
	"reflect"
	"testing"
)

func TestStripJSONComments_LineComment(t *testing.T) {
	got := string(Strip([]byte("{\n  // a comment\n  \"a\": 1\n}")))
	want := "{\n  \n  \"a\": 1\n}"
	if got != want {
		t.Errorf("Strip: got %q, want %q", got, want)
	}
}

func TestStripJSONComments_BlockComment(t *testing.T) {
	got := string(Strip([]byte(`{/* hi */"a": 1}`)))
	if got != `{"a": 1}` {
		t.Errorf("Strip: got %q", got)
	}
}

func TestStripJSONComments_PreservesSequencesInsideStrings(t *testing.T) {
	in := `{"url": "https://example.com//x", "glob": "src/**/*", "block": "/* not a comment */"}`
	if got := string(Strip([]byte(in))); got != in {
		t.Errorf("Strip altered string literals:\n got %s\nwant %s", got, in)
	}
}

func TestStripJSONComments_PreservesEscapedQuote(t *testing.T) {
	in := `{"a": "he said \"//\"", "b": 1}`
	if got := string(Strip([]byte(in))); got != in {
		t.Errorf("Strip altered escaped string:\n got %s\nwant %s", got, in)
	}
}

func TestStripJSONComments_TrailingCommas(t *testing.T) {
	var v struct {
		A []int          `json:"a"`
		B map[string]int `json:"b"`
	}
	if err := Unmarshal([]byte("{\"a\": [1, 2,],\n \"b\": {\"k\": 1,},\n}"), &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(v.A, []int{1, 2}) || v.B["k"] != 1 {
		t.Errorf("decoded %+v", v)
	}
}

func TestStripJSONComments_UnterminatedBlockComment(t *testing.T) {
	// Must not panic or loop forever; the truncated input stays invalid JSON.
	var v map[string]any
	if err := Unmarshal([]byte(`{"a": 1 /* oops`), &v); err == nil {
		t.Error("expected an error for unterminated block comment")
	}
}
