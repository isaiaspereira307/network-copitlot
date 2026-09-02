package extensions

import "testing"

func TestEnableDisableList(t *testing.T) {
	m := New(t.TempDir())
	// sem enable, lista com enabled=false
	list := m.List("proj")
	if len(list) == 0 {
		t.Fatal("esperava extensions builtin")
	}
	if err := m.Enable("proj", "aws-key-secret"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.Enable("proj", "nao-existe"); err == nil {
		t.Fatal("enable de ext inexistente deveria falhar")
	}
	en := m.EnabledExtensions("proj")
	if len(en) != 1 || en[0].Name() != "aws-key-secret" {
		t.Fatalf("EnabledExtensions: %+v", en)
	}
	if err := m.Disable("proj", "aws-key-secret"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(m.EnabledExtensions("proj")) != 0 {
		t.Fatal("esperava 0 enabled apos disable")
	}
}

func TestAwsKeyExtOnFinding(t *testing.T) {
	e := awsKeyExt{}
	var emitted []map[string]any
	err := e.OnFinding(HookContext{Type: "on_finding", URL: "https://x/a",
		Evidence: map[string]any{"secret": "AKIA1234567890ABCDEF"}}, func(m map[string]any) {
		emitted = append(emitted, m)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 1 {
		t.Fatalf("esperava 1 finding emitido, got %d", len(emitted))
	}
	if emitted[0]["severity"] != "crit" {
		t.Fatalf("severity invalida: %v", emitted[0])
	}
	// chave curta / nao-AWS nao deve emitir
	e.OnFinding(HookContext{Type: "on_finding", Evidence: map[string]any{"secret": "abc"}},
		func(m map[string]any) { emitted = append(emitted, m) })
	if len(emitted) != 1 {
		t.Fatalf("nao deveria emitir p/ chave curta, got %d", len(emitted))
	}
}
