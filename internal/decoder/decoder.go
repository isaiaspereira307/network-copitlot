// Package decoder implements v5.0 encode/decode multi-formato (base64, URL,
// hex, HTML entities, JWT payload, gzip). Puro-Go, sem dependencias externas.
package decoder

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
