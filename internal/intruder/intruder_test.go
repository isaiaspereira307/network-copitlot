package intruder

import (
	"reflect"
	"testing"
)

func TestGenerateSniper(t *testing.T) {
	cases, err := Generate(AttackSniper,
		[]Position{{Kind: "query:id"}, {Kind: "query:q"}},
		[][]string{{"A", "B"}, {"X", "Y", "Z"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Sniper: cada set uma posicao por vez; set 0 (2 payloads) sobre pos 0,
	// set 1 (3 payloads) sobre pos 1 => 2+3 = 5 casos.
	if len(cases) != 5 {
		t.Fatalf("expected 5 cases, got %d", len(cases))
	}
	want := []Case{
		{Payloads: []string{"A", ""}},
		{Payloads: []string{"B", ""}},
		{Payloads: []string{"", "X"}},
		{Payloads: []string{"", "Y"}},
		{Payloads: []string{"", "Z"}},
	}
	if !reflect.DeepEqual(cases, want) {
		t.Fatalf("unexpected: %+v", cases)
	}
}

func TestGenerateBatteringRam(t *testing.T) {
	cases, err := Generate(AttackBatteringRam,
		[]Position{{Kind: "query:id"}, {Kind: "query:q"}},
		[][]string{{"A", "B", "C"}, {"D"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Ram usa o payload do primeiro set (por convencao) em todas posicoes.
	if len(cases) != 3 {
		t.Fatalf("expected 3, got %d", len(cases))
	}
	for i, want := range []string{"A", "B", "C"} {
		if cases[i].Payloads[0] != want || cases[i].Payloads[1] != want {
			t.Fatalf("ram line %d = %v", i, cases[i].Payloads)
		}
	}
}

func TestGeneratePitchfork(t *testing.T) {
	cases, err := Generate(AttackPitchfork,
		[]Position{{Kind: "query:id"}, {Kind: "query:q"}},
		[][]string{{"A", "B"}, {"X", "Y", "Z"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Pitchfork: linha i usa payload i de cada set (até o len máximo).
	if len(cases) != 3 {
		t.Fatalf("expected 3, got %d", len(cases))
	}
	want := []Case{
		{Payloads: []string{"A", "X"}},
		{Payloads: []string{"B", "Y"}},
		{Payloads: []string{"", "Z"}}, // set 0 esgotado -> ""
	}
	if !reflect.DeepEqual(cases, want) {
		t.Fatalf("unexpected: %+v", cases)
	}
}

func TestGenerateClusterBomb(t *testing.T) {
	cases, err := Generate(AttackClusterBomb,
		[]Position{{Kind: "query:id"}, {Kind: "query:q"}},
		[][]string{{"A", "B"}, {"X", "Y"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cases) != 4 { // 2*2
		t.Fatalf("expected 4, got %d", len(cases))
	}
	want := []Case{
		{Payloads: []string{"A", "X"}},
		{Payloads: []string{"B", "X"}},
		{Payloads: []string{"A", "Y"}},
		{Payloads: []string{"B", "Y"}},
	}
	if !reflect.DeepEqual(cases, want) {
		t.Fatalf("unexpected: %+v", cases)
	}
}

func TestGenerateErrors(t *testing.T) {
	if _, err := Generate(AttackSniper, nil, [][]string{{"a"}}); err == nil {
		t.Fatalf("expected error on no positions")
	}
	if _, err := Generate(AttackSniper, []Position{{Kind: "url"}}, [][]string{{"a"}, {"b"}}); err == nil {
		t.Fatalf("expected error on set/position mismatch")
	}
	if _, err := Generate(AttackSniper, []Position{{Kind: "url"}}, [][]string{}); err == nil {
		t.Fatalf("expected error on no sets")
	}
	if _, err := Generate(AttackType("bogus"), []Position{{Kind: "url"}}, [][]string{{"a"}}); err == nil {
		t.Fatalf("expected error on bad attack")
	}
}

func TestParsePosition(t *testing.T) {
	if _, err := ParsePosition("query:"); err == nil {
		t.Fatalf("expected error on empty query name")
	}
	if p, err := ParsePosition("header:X-Id"); err != nil || p.Name != "X-Id" {
		t.Fatalf("unexpected: %+v %v", p, err)
	}
	if _, err := ParsePosition("nonsense"); err == nil {
		t.Fatalf("expected error on bad position")
	}
}
