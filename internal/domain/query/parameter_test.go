package query_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

func TestTypedValueValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   query.TypedValue
		wantErr bool
	}{
		// string — any text is a valid string
		{"string empty", query.TypedValue{Type: query.ParamString, Text: ""}, false},
		{"string plain", query.TypedValue{Type: query.ParamString, Text: "hello"}, false},
		{"string quotes", query.TypedValue{Type: query.ParamString, Text: "it's"}, false},

		// integer — int64 decimal form only
		{"integer zero", query.TypedValue{Type: query.ParamInteger, Text: "0"}, false},
		{"integer negative", query.TypedValue{Type: query.ParamInteger, Text: "-42"}, false},
		{"integer max int64", query.TypedValue{Type: query.ParamInteger, Text: "9223372036854775807"}, false},
		{"integer overflow", query.TypedValue{Type: query.ParamInteger, Text: "9223372036854775808"}, true},
		{"integer empty", query.TypedValue{Type: query.ParamInteger, Text: ""}, true},
		{"integer float form", query.TypedValue{Type: query.ParamInteger, Text: "1.5"}, true},
		{"integer hex", query.TypedValue{Type: query.ParamInteger, Text: "0x10"}, true},
		{"integer spaces", query.TypedValue{Type: query.ParamInteger, Text: " 1"}, true},
		{"integer letters", query.TypedValue{Type: query.ParamInteger, Text: "abc"}, true},

		// decimal — exact decimal text, optional exponent; no NaN/Inf/underscores
		{"decimal int form", query.TypedValue{Type: query.ParamDecimal, Text: "0"}, false},
		{"decimal plain", query.TypedValue{Type: query.ParamDecimal, Text: "1.5"}, false},
		{"decimal negative", query.TypedValue{Type: query.ParamDecimal, Text: "-0.001"}, false},
		{"decimal leading dot", query.TypedValue{Type: query.ParamDecimal, Text: ".5"}, false},
		{"decimal trailing dot", query.TypedValue{Type: query.ParamDecimal, Text: "5."}, false},
		{"decimal exponent", query.TypedValue{Type: query.ParamDecimal, Text: "1e10"}, false},
		{"decimal signed exponent", query.TypedValue{Type: query.ParamDecimal, Text: "1.5E-3"}, false},
		{"decimal plus sign", query.TypedValue{Type: query.ParamDecimal, Text: "+2"}, false},
		{"decimal empty", query.TypedValue{Type: query.ParamDecimal, Text: ""}, true},
		{"decimal letters", query.TypedValue{Type: query.ParamDecimal, Text: "abc"}, true},
		{"decimal NaN", query.TypedValue{Type: query.ParamDecimal, Text: "NaN"}, true},
		{"decimal Inf", query.TypedValue{Type: query.ParamDecimal, Text: "Inf"}, true},
		{"decimal double dot", query.TypedValue{Type: query.ParamDecimal, Text: "1..2"}, true},
		{"decimal bare exponent", query.TypedValue{Type: query.ParamDecimal, Text: "1e"}, true},
		{"decimal double sign", query.TypedValue{Type: query.ParamDecimal, Text: "--1"}, true},
		{"decimal underscore", query.TypedValue{Type: query.ParamDecimal, Text: "1_000"}, true},
		{"decimal dot only", query.TypedValue{Type: query.ParamDecimal, Text: "."}, true},

		// boolean — strict lowercase true/false
		{"boolean true", query.TypedValue{Type: query.ParamBoolean, Text: "true"}, false},
		{"boolean false", query.TypedValue{Type: query.ParamBoolean, Text: "false"}, false},
		{"boolean upper", query.TypedValue{Type: query.ParamBoolean, Text: "TRUE"}, true},
		{"boolean short", query.TypedValue{Type: query.ParamBoolean, Text: "t"}, true},
		{"boolean numeric", query.TypedValue{Type: query.ParamBoolean, Text: "1"}, true},
		{"boolean empty", query.TypedValue{Type: query.ParamBoolean, Text: ""}, true},

		// date — ISO-8601 calendar date only
		{"date valid", query.TypedValue{Type: query.ParamDate, Text: "2026-07-19"}, false},
		{"date bad month", query.TypedValue{Type: query.ParamDate, Text: "2026-13-01"}, true},
		{"date us format", query.TypedValue{Type: query.ParamDate, Text: "07/19/2026"}, true},
		{"date with time", query.TypedValue{Type: query.ParamDate, Text: "2026-07-19T00:00:00Z"}, true},
		{"date empty", query.TypedValue{Type: query.ParamDate, Text: ""}, true},

		// timestamp — RFC 3339 with offset
		{"timestamp utc", query.TypedValue{Type: query.ParamTimestamp, Text: "2026-07-19T12:34:56Z"}, false},
		{"timestamp offset fraction", query.TypedValue{Type: query.ParamTimestamp, Text: "2026-07-19T12:34:56.123+09:00"}, false},
		{"timestamp date only", query.TypedValue{Type: query.ParamTimestamp, Text: "2026-07-19"}, true},
		{"timestamp space separator", query.TypedValue{Type: query.ParamTimestamp, Text: "2026-07-19 12:34:56"}, true},
		{"timestamp empty", query.TypedValue{Type: query.ParamTimestamp, Text: ""}, true},

		// uuid — canonical hyphenated form, any case hex
		{"uuid lower", query.TypedValue{Type: query.ParamUUID, Text: "3b241101-e2bb-4255-8caf-4136c566a962"}, false},
		{"uuid upper", query.TypedValue{Type: query.ParamUUID, Text: "3B241101-E2BB-4255-8CAF-4136C566A962"}, false},
		{"uuid no hyphens", query.TypedValue{Type: query.ParamUUID, Text: "3b241101e2bb42558caf4136c566a962"}, true},
		{"uuid bad hex", query.TypedValue{Type: query.ParamUUID, Text: "zb241101-e2bb-4255-8caf-4136c566a962"}, true},
		{"uuid braces", query.TypedValue{Type: query.ParamUUID, Text: "{3b241101-e2bb-4255-8caf-4136c566a962}"}, true},
		{"uuid empty", query.TypedValue{Type: query.ParamUUID, Text: ""}, true},

		// null — Text must be empty
		{"null empty", query.TypedValue{Type: query.ParamNull, Text: ""}, false},
		{"null with text", query.TypedValue{Type: query.ParamNull, Text: "x"}, true},

		// unknown type
		{"bogus type", query.TypedValue{Type: query.ParamType("bogus"), Text: "1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.value.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, query.ErrInvalidParamValue) {
				t.Fatalf("Validate() error = %v, want errors.Is(err, ErrInvalidParamValue)", err)
			}
			// Parameter values are sensitive (PRD §8.1): the error text must never
			// echo the value back. Values shorter than 3 bytes are skipped — they
			// collide with ordinary English words in the message.
			if len(tt.value.Text) >= 3 && strings.Contains(err.Error(), tt.value.Text) {
				t.Fatalf("Validate() error %q leaks the parameter value %q", err, tt.value.Text)
			}
		})
	}
}
