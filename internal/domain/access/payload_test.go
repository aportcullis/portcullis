package access_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

func TestNewPayload(t *testing.T) {
	t.Parallel()
	p, err := access.NewPayload("select * from t where id = :id", []query.Parameter{
		{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "7"}},
	})
	if err != nil {
		t.Fatalf("NewPayload: %v", err)
	}
	if p.SQL == "" || len(p.Params) != 1 {
		t.Errorf("payload shape: %+v", p)
	}

	cases := []struct {
		name   string
		sql    string
		params []query.Parameter
	}{
		{"empty sql", "", nil},
		{"whitespace sql", "  \n\t ", nil},
		{"sql over the budget", "select " + strings.Repeat("x", access.MaxPayloadBytes), nil},
		// The budget covers the WHOLE payload, not the SQL alone: parameter values are the other half of what gets encrypted, shipped, and executed, so a tiny statement with huge values must not slip through.
		{"parameters over the budget", "select :big", []query.Parameter{
			{Name: "big", Value: query.TypedValue{Type: query.ParamString, Text: strings.Repeat("v", access.MaxPayloadBytes)}},
		}},

		{"names over the budget", strings.Repeat("x", access.MaxPayloadBytes-100), manyParams(100)},
		{"unnamed param", "select :id", []query.Parameter{{Name: "", Value: query.TypedValue{Type: query.ParamInteger, Text: "1"}}}},
		{"duplicate param", "select :id", []query.Parameter{
			{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "1"}},
			{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "2"}},
		}},
		{"invalid value", "select :id", []query.Parameter{{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "seven"}}}},
		{"too many params", "select 1", manyParams(access.MaxPayloadParams + 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := access.NewPayload(tc.sql, tc.params); !errors.Is(err, access.ErrInvalidPayload) {
				t.Errorf("NewPayload = %v, want ErrInvalidPayload", err)
			}
		})
	}
}

func TestPayloadAtTheBudgetIsAccepted(t *testing.T) {
	t.Parallel()
	param := query.Parameter{Name: "id", Value: query.TypedValue{Type: query.ParamString, Text: "seven"}}
	overhead := len(param.Name) + len(param.Value.Text)
	sql := "select " + strings.Repeat("x", access.MaxPayloadBytes-len("select ")-overhead)

	p, err := access.NewPayload(sql, []query.Parameter{param})
	if err != nil {
		t.Fatalf("payload exactly at the budget = %v, want accepted", err)
	}
	if len(p.SQL)+overhead != access.MaxPayloadBytes {
		t.Fatalf("test set up %d bytes, want exactly %d", len(p.SQL)+overhead, access.MaxPayloadBytes)
	}

	if _, err := access.NewPayload(sql+"x", []query.Parameter{param}); !errors.Is(err, access.ErrInvalidPayload) {
		t.Errorf("one byte over the budget = %v, want ErrInvalidPayload", err)
	}
}

func manyParams(n int) []query.Parameter {
	ps := make([]query.Parameter, n)
	for idx := range ps {
		ps[idx] = query.Parameter{Name: "p" + strings.Repeat("x", idx%5) + string(rune('a'+idx%26)) + itoa(idx), Value: query.TypedValue{Type: query.ParamNull}}
	}
	return ps
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
