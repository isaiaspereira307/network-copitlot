// Package decoder implements v5.0 encode/decode multi-formato (base64, URL,
// hex, HTML entities, JWT payload, gzip). Puro-Go, sem dependencias externas.
package decoder

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Formats suportados por Decode/Encode.
var Formats = []string{"base64", "url", "hex", "html", "jwt", "gzip"}

// Decode decodifica input conforme o formato. JWT aceita "alg.payload.sig" e
// decodifica o payload; gzip espera bytes gzip.
func Decode(format, input string) (string, error) {
	switch strings.ToLower(format) {
	case "base64":
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(input))
		if err != nil {
			// tenta base64url (JWT-friendly)
			b2, e2 := base64.RawURLEncoding.DecodeString(strings.TrimSpace(input))
			if e2 != nil {
				return "", fmt.Errorf("base64 invalido: %w", err)
			}
			return string(b2), nil
		}
		return string(b), nil
	case "url":
		return url.QueryUnescape(input)
	case "hex":
		b, err := hex.DecodeString(strings.TrimSpace(input))
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "html":
		return htmlUnescape(input), nil
	case "jwt":
		return decodeJWT(input)
	case "gzip":
		// Encode devolve base64; aceita base64 OU bytes crus.
		raw := []byte(input)
		if b, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(input)); derr == nil {
			raw = b
		}
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
		defer zr.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(zr); err != nil {
			return "", err
		}
		return buf.String(), nil
	default:
		return "", fmt.Errorf("formato invalido %q: use %s", format, strings.Join(Formats, ", "))
	}
}

// Encode codifica input conforme o formato. gzip devolve base64 dos bytes.
func Encode(format, input string) (string, error) {
	switch strings.ToLower(format) {
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(input)), nil
	case "url":
		return url.QueryEscape(input), nil
	case "hex":
		return hex.EncodeToString([]byte(input)), nil
	case "html":
		return htmlEscape(input), nil
	case "jwt":
		// re-encoda apenas o payload sobre um header HS256 ficticio
		return encodeJWTPayload(input), nil
	case "gzip":
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write([]byte(input)); err != nil {
			return "", err
		}
		if err := zw.Close(); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
	default:
		return "", fmt.Errorf("formato invalido %q: use %s", format, strings.Join(Formats, ", "))
	}
}

func decodeJWT(token string) (string, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("jwt invalido: esperado <alg>.<payload>[.<sig>]")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(parts[1], "="))
	if err != nil {
		// aceita padding
		raw, err = base64.URLEncoding.DecodeString(withPadding(parts[1]))
		if err != nil {
			return "", err
		}
	}
	// compacta o JSON p/ frugalidade de tokens
	var pretty bytes.Buffer
	if json.Indent(&pretty, raw, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(raw), nil
}

// JWTInfo e o resultado completo do decode de um JWT (header, payload,
// assinatura e warnings de superficie de ataque).
type JWTInfo struct {
	Header    string   `json:"header"`
	Payload   string   `json:"payload"`
	Signature string   `json:"signature"`
	Warnings  []string `json:"warnings"`
}

// DecodeJWTFull decodifica header+payload de um JWT (JWS compact) e devolve
// warnings de ataque: alg=none, assinatura vazia, exp expirado.
func DecodeJWTFull(token string) (*JWTInfo, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("jwt invalido: esperado header.payload[.sig]")
	}
	header, err := b64url(parts[0])
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	payload, err := b64url(parts[1])
	if err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}
	sig := ""
	if len(parts) > 2 {
		if b, err := base64.RawURLEncoding.DecodeString(parts[2]); err == nil {
			sig = string(b)
		} else {
			sig = parts[2] // opaque
		}
	}
	info := &JWTInfo{Header: header, Payload: payload, Signature: sig}
	var hdr struct {
		Alg string `json:"alg"`
	}
	_ = json.Unmarshal([]byte(header), &hdr) // best-effort
	if strings.EqualFold(hdr.Alg, "none") {
		info.Warnings = append(info.Warnings, "alg=none")
	}
	if len(parts) < 3 || parts[2] == "" {
		info.Warnings = append(info.Warnings, "assinatura vazia")
	}
	var pl struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal([]byte(payload), &pl); err == nil && pl.Exp > 0 && pl.Exp < time.Now().Unix() {
		info.Warnings = append(info.Warnings, "exp expirado")
	}
	return info, nil
}

// b64url decodifica base64url com ou sem padding (mesma tolerancia de decodeJWT).
func b64url(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(s, "="))
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(withPadding(s))
		if err != nil {
			return "", err
		}
	}
	return string(b), nil
}

// ResignJWT re-assina um JWT offline para acceptance testing: alg=none troca o
// header (alg:none, sem assinatura, token termina com '.'); hs256 assina com
// HMAC-SHA256(secret, header.payload). Payload original preservado.
func ResignJWT(token, alg, secret string) (string, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("jwt invalido")
	}
	switch strings.ToLower(alg) {
	case "none":
		hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
		return hdr + "." + parts[1] + ".", nil
	case "hs256":
		hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
		signing := hdr + "." + parts[1]
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(signing))
		return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
	default:
		return "", fmt.Errorf("alg %q nao suportado: use none ou hs256", alg)
	}
}

func encodeJWTPayload(input string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(input))
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	return header + "." + payload + "."
}

func withPadding(s string) string {
	for len(s)%4 != 0 {
		s += "="
	}
	return s
}

// Minimal HTML entity unescape/escape (cobre os mais comuns e numericos).
func htmlUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
		"&apos;", "'", "&nbsp;", " ", "&#x27;", "'", "&#39;", "'",
	)
	out := r.Replace(s)
	// numericos &#...; e &#x...;
	i := 0
	var b strings.Builder
	for i < len(out) {
		rest := out[i:]
		if strings.HasPrefix(rest, "&#") {
			end := strings.IndexByte(rest, ';')
			if end > 2 {
				num := rest[2:end]
				var r rune
				if strings.HasPrefix(num, "x") || strings.HasPrefix(num, "X") {
					var v uint64
					fmt.Sscanf(num[1:], "%x", &v)
					r = rune(v)
				} else {
					var v uint64
					fmt.Sscanf(num, "%d", &v)
					r = rune(v)
				}
				b.WriteRune(r)
				i += end + 1
				continue
			}
		}
		b.WriteByte(rest[0])
		i++
	}
	return b.String()
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}
