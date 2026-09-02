package macro

import "testing"

func TestSubstitute(t *testing.T) {
	vars := map[string]string{"token": "abc123", "id": "42"}
	got := Substitute("POST /users/{id} Authorization: {token} missing:{nope}", vars)
	want := "POST /users/42 Authorization: abc123 missing:{nope}"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExtractVar(t *testing.T) {
	ex := Extractor{Name: "csrftoken", Pattern: `name="csrf" value="([^"]+)"`}
	body := []byte(`<input name="csrf" value="XYZ">`)
	v, ok := extractVar(body, ex)
	if !ok || v != "XYZ" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
	// sem grupo -> falha
	ex2 := Extractor{Name: "x", Pattern: `[0-9]+`}
	if _, ok := extractVar(body, ex2); ok {
		t.Fatalf("expected no match without capture group")
	}
}

func TestSaveLoadList(t *testing.T) {
	m := NewManager(t.TempDir())
	mac := &Macro{Name: "login",
		Steps: []Step{{
			Method: "POST",
			URL:    "https://api.example/login",
			Body:   "user=u&pass=p",
			Extractors: []Extractor{
				{Name: "token", Pattern: `"token":"([^"]+)"`},
			},
		}},
	}
	if err := m.Save(mac); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := m.Load("login")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if back.ID == "" || back.Steps[0].Extractors[0].Name != "token" {
		t.Fatalf("bad roundtrip: %+v", back)
	}
	names, _ := m.List()
	if len(names) != 1 || names[0] != "login" {
		t.Fatalf("bad list: %v", names)
	}
}

func TestPlaySession(t *testing.T) {
	m := NewManager(t.TempDir())
	mac := &Macro{Name: "auth",
		Steps: []Step{
			{Method: "GET", URL: "https://api.example/csrf",
				Extractors: []Extractor{{Name: "csrf", Pattern: `csrf=([0-9a-f]+)`}}},
			{Method: "POST", URL: "https://api.example/{csrf}/login", Body: "{csrf}"},
		},
	}
	m.Save(mac)

	runner := func(step Step, vars map[string]string) ([]byte, int, error) {
		if step.URL == "https://api.example/csrf" {
			return []byte("csrf=deadbeef"), 200, nil
		}
		return []byte("ok"), 200, nil
	}
	res, err := m.Play("auth", "", runner)
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if res.Vars["csrf"] != "deadbeef" {
		t.Fatalf("var not extracted: %+v", res.Vars)
	}
}
