package media2

import (
	"encoding/xml"
	"errors"
	"testing"
)

func TestDecodeGovLengthRangeEnforcesXSIntBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		value   string
		want    IntRange
		wantErr bool
	}{
		{
			name:  "signed boundaries",
			value: "-2147483648 2147483647",
			want:  IntRange{Min: -2147483648, Max: 2147483647},
		},
		{name: "positive overflow", value: "1 2147483648", wantErr: true},
		{name: "negative overflow", value: "-2147483649 1", wantErr: true},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			options := &element{attrs: []xml.Attr{{Name: xml.Name{Local: "GovLengthRange"}, Value: testCase.value}}}
			got, err := decodeGovLengthRange(options)
			if testCase.wantErr {
				if !errors.Is(err, ErrInvalidField) {
					t.Fatalf("decodeGovLengthRange() error = %v, want ErrInvalidField", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeGovLengthRange() error = %v", err)
			}
			if got == nil || *got != testCase.want {
				t.Fatalf("decodeGovLengthRange() = %+v, want %+v", got, testCase.want)
			}
		})
	}
}
