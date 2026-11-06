package search

import (
	"reflect"
	"strings"
	"testing"
)

func TestWholeWordsNormalizeUnicodeWithoutStemming(t *testing.T) {
	want := []string{"contratos", "contrato", "acao", "a"}
	got := Words("AÇÃO, ação! contrato contratos a")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("words: %v, want %v", got, want)
	}
	if !reflect.DeepEqual(Words("ação"), Words("AÇÃO")) {
		t.Fatal("decomposed Unicode differs")
	}
	if !reflect.DeepEqual(Words("ana.silva@example.com"), []string{"example", "silva", "ana", "com"}) {
		t.Fatal("email words differ")
	}
}

func TestSearchValidation(t *testing.T) {
	for _, value := range []string{"!!!", strings.Repeat("a", 201), "a b c d e f g h i"} {
		if _, err := Parse(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, value := range []string{"", " ", "a", "a b c d e f g h", "a a a a a a a a a"} {
		if _, err := Parse(value); err != nil {
			t.Fatalf("rejected %q: %v", value, err)
		}
	}
}
