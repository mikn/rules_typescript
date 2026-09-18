package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
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
	in := []byte("{\"a\": 1 /* oops\nand more\n")
	var v map[string]any
	if err := Unmarshal(in, &v); err == nil {
		t.Error("expected an error for unterminated block comment")
	}
	if got, want := lineOf(Strip(in), -1), lineOf(in, -1); got != want {
		t.Errorf("Strip: %d lines, want %d", got, want)
	}
}

// The line a parse error is reported on comes from the stripped text, so
// dropping a comment or a trailing comma must not drop its newlines with it.
func TestStripJSONComments_ParseErrorKeepsTheSourceLine(t *testing.T) {
	const src = `{
  /* A block comment
     of several lines,
     above the mistake. */
  "a": {
    "x": 1,
  },
  "b": 2
  "c": 3
}
`
	const badLine = 9

	stripped := Strip([]byte(src))
	if got, want := lineOf(stripped, -1), lineOf([]byte(src), -1); got != want {
		t.Errorf("Strip: %d lines, want %d\n%s", got, want, stripped)
	}

	var syntax *json.SyntaxError
	if err := json.Unmarshal(stripped, new(map[string]any)); !errors.As(err, &syntax) {
		t.Fatalf("Unmarshal: got %v, want a *json.SyntaxError", err)
	}
	if got := lineOf(stripped, int(syntax.Offset)); got != badLine {
		t.Errorf("parse error reported on line %d, want %d", got, badLine)
	}
}

// lineOf reports the 1-based line data[offset] is on; a negative offset means
// the end of data.
func lineOf(data []byte, offset int) int {
	if offset < 0 {
		offset = len(data)
	}
	return 1 + bytes.Count(data[:offset], []byte("\n"))
}
