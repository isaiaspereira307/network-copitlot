package decoder

import (
	"slices"
	"strings"
	"testing"
)

func TestDecodeBase64(t *testing.T) {
	got, err := Decode("base64", "aGVsbG8gd29ybGQ=")
	if err != nil || got != "hello world" {
		t.Fatalf("base64 decode: got %q err %v", got, err)
	}
}

func TestDecodeURL(t *testing.T) {
	got, err := Decode("url", "a%20b%2Fc")
	if err != nil || got != "a b/c" {
		t.Fatalf("url decode: got %q err %v", got, err)
	}
}

func TestDecodeHex(t *testing.T) {
	got, err := Decode("hex", "68656c6c6f")
	if err != nil || got != "hello" {
		t.Fatalf("hex decode: got %q err %v", got, err)
	}
}

func TestDecodeHTML(t *testing.T) {
	got, err := Decode("html", "&lt;b&gt;&amp;&#39;x&#39;&lt;/b&gt;")
	if err != nil || got != "<b>&'x'</b>" {
		t.Fatalf("html decode: got %q err %v", got, err)
	}
}

func TestDecodeJWT(t *testing.T) {
	// payload {"a":1}
	token := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJhIjoxfQ.sig"
	got, err := Decode("jwt", token)
	if err != nil {
		t.Fatalf("jwt decode: %v", err)
	}
	if !strings.Contains(got, `"a": 1`) && !strings.Contains(got, `"a":1`) {
		t.Fatalf("jwt payload nao ok: %q", got)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, f := range []string{"base64", "url", "hex", "html"} {
		enc, err := Encode(f, "valor com <espacos> & 'quotes'")
		if err != nil {
			t.Fatalf("encode %s: %v", f, err)
		}
		dec, err := Decode(f, enc)
		if err != nil || dec != "valor com <espacos> & 'quotes'" {
			t.Fatalf("roundtrip %s: got %q err %v", f, dec, err)
		}
	}
}

func TestDecodeGzip(t *testing.T) {
	enc, err := Encode("gzip", "dados comprimidos")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decode("gzip", enc)
	if err != nil || dec != "dados comprimidos" {
		t.Fatalf("gzip roundtrip: got %q err %v", dec, err)
	}
}

func TestDecodeJWTFull(t *testing.T) {
	// header {"alg":"HS256","typ":"JWT"}, payload {"sub":"u1","exp":1000000000} (expirado)
	tok := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1MSIsImV4cCI6MTAwMDAwMDAwMH0.sig"
	info, err := DecodeJWTFull(tok)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Header, "HS256") || !strings.Contains(info.Payload, `"sub":"u1"`) {
		t.Errorf("header=%s payload=%s", info.Header, info.Payload)
	}
	if !slices.Contains(info.Warnings, "exp expirado") {
		t.Errorf("warnings = %v, want exp expirado", info.Warnings)
	}
	// alg=none
	noneTok := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ1MSJ9."
	info2, err := DecodeJWTFull(noneTok)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(info2.Warnings, "alg=none") || !slices.Contains(info2.Warnings, "assinatura vazia") {
		t.Errorf("warnings = %v, want alg=none e assinatura vazia", info2.Warnings)
	}
	// token invalido
	if _, err := DecodeJWTFull("nao-e-jwt"); err == nil {
		t.Error("esperava erro para token invalido")
	}
}
