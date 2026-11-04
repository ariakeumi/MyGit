package github

import (
	"reflect"
	"testing"
)

func TestLoginOutputExtractsOnlyDeviceCode(t *testing.T) {
	var codes []string
	out := &loginOutput{code: func(code string) { codes = append(codes, code) }}
	for _, chunk := range []string{
		"ABCD-EFGH is unrelated output\n",
		"! First copy your one-time co",
		"de: A1B2-C3D4\n",
		"! First copy your one-time code: XXXX-YYYY\n",
	} {
		if n, err := out.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("write returned %d, %v", n, err)
		}
	}
	if want := []string{"A1B2-C3D4"}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes %v, want %v", codes, want)
	}
}
