// Package intruder implements the v3.0 fuzzing engine: position-swapping
// over payload sets (Sniper, Battering Ram, Pitchfork, Cluster Bomb), built on
// the existing store.Replay scope-guarded replay path.
package intruder

import (
	"fmt"
	"strings"
)

// AttackType enumera os 4 modos de ataque do intruder (PRD v3.0):
//   - Sniper:      um set de payloads, cada posicao uma vez (N*n requests)
//   - BatteringRam: um set, todos marcadores recebem o MESMO payload por linha (N requests)
//   - Pitchfork:   sets paralelos, linha i usa payload i de cada set (maxlen requests)
//   - ClusterBomb: produto cartesiano dos sets (n1*n2*... requests)
type AttackType string

const (
	AttackSniper       AttackType = "sniper"
	AttackBatteringRam AttackType = "battering_ram"
	AttackPitchfork    AttackType = "pitchfork"
	AttackClusterBomb  AttackType = "cluster_bomb"
)

func (a AttackType) Valid() bool {
	switch a {
	case AttackSniper, AttackBatteringRam, AttackPitchfork, AttackClusterBomb:
		return true
	}
	return false
}

// Position identifica um ponto de injecao num request base: a URL completa, o
// body completo, um parametro de query, ou um header. Posicoes sao 1-indexadas
// no genero do request (o AI referencia por indice no request original).
type Position struct {
	// Kind: url | body | query:<name> | header:<name>
	Kind string
	Part int // indice do segmento/valor alvo dentro do genero, 1-based
	Name string
}

// ParsePosition converte "query:id", "header:X-IP", "url", "body" num Position.
func ParsePosition(point string) (Position, error) {
	switch {
	case point == "url":
		return Position{Kind: "url", Part: 1}, nil
	case point == "body":
		return Position{Kind: "body", Part: 1}, nil
	case strings.HasPrefix(point, "query:"):
		name := strings.TrimPrefix(point, "query:")
		if name == "" {
			return Position{}, fmt.Errorf("query position sem nome: use query:<param>")
		}
		return Position{Kind: "query:" + name, Part: 1, Name: name}, nil
	case strings.HasPrefix(point, "header:"):
		name := strings.TrimPrefix(point, "header:")
		if name == "" {
			return Position{}, fmt.Errorf("header position sem nome: use header:<nome>")
		}
		return Position{Kind: "header:" + name, Part: 1, Name: name}, nil
	}
	return Position{}, fmt.Errorf("position invalida %q: url | body | query:<param> | header:<nome>", point)
}

// PayloadSet e um conjunto nomeado de payloads (um por posicao no multi-pos).
type PayloadSet struct {
	// Bytes opcionais por payload (mutacao binaria); nil = string payload.
	Values []string
}

// Case representa uma linha de fuzzing: a combinacao de payloads (1 por
// posicao no genero) que sera injetada e reenviada como um request.
type Case struct {
	// Payloads[i] corresponde a posicao i do ataque.
	Payloads []string
}

// Generate monta a lista de casos a executar dado o modo de ataque e os sets de
// payload. positions = lista de posicoes a fuzzar (tamanho = ordens de set).
func Generate(attack AttackType, positions []Position, sets [][]string) ([]Case, error) {
	if len(positions) == 0 {
		return nil, fmt.Errorf("nenhuma position de fuzz")
	}
	if len(sets) == 0 {
		return nil, fmt.Errorf("nenhum payload set")
	}
	order := len(positions)
	if len(sets) != order {
		return nil, fmt.Errorf("num payload sets (%d) != num positions (%d)", len(sets), order)
	}
	for _, set := range sets {
		if len(set) == 0 {
			return nil, fmt.Errorf("payload set vazio")
		}
	}

	var cases []Case
	switch attack {
	case AttackSniper:
		// Cada set e aplicado uma posicao por vez; as demais usa valor original.
		for i, set := range sets {
			for _, p := range set {
				c := make([]string, order)
				for j := range c {
					if j != i {
						c[j] = "" // marcador: manter original
					}
				}
				c[i] = p
				cases = append(cases, Case{Payloads: c})
			}
		}
	case AttackBatteringRam:
		// Linha k usa o payload k de cada set em TODAS as posicoes alteraveis
		// (o mesmo payload para todos os marcadores da linha).
		maxLen := 0
		for _, set := range sets {
			if len(set) > maxLen {
				maxLen = len(set)
			}
		}
		for k := 0; k < maxLen; k++ {
			val := ""
			if k < len(sets[0]) {
				val = sets[0][k]
			}
			c := make([]string, order)
			for i := range c {
				c[i] = val
			}
			cases = append(cases, Case{Payloads: c})
		}
	case AttackPitchfork:
		// Linha i usa o payload i de cada set; para cada set, este eh o mesmo index.
		maxLen := 0
		for _, set := range sets {
			if len(set) > maxLen {
				maxLen = len(set)
			}
		}
		for i := 0; i < maxLen; i++ {
			c := make([]string, order)
			for j, set := range sets {
				if i < len(set) {
					c[j] = set[i]
				} else {
					c[j] = ""
				}
			}
			cases = append(cases, Case{Payloads: c})
		}
	case AttackClusterBomb:
		// Produto cartesiano.
		total := 1
		for _, set := range sets {
			total *= len(set)
		}
		cases = make([]Case, 0, total)
		idx := make([]int, order)
		for {
			c := make([]string, order)
			for j, set := range sets {
				c[j] = set[idx[j]]
			}
			cases = append(cases, Case{Payloads: c})
			// incrementa o odometro
			carry := 1
			for j := 0; j < order && carry > 0; j++ {
				idx[j] += carry
				if idx[j] >= len(sets[j]) {
					idx[j] = 0
				} else {
					carry = 0
				}
			}
			if carry > 0 { // overflow
				break
			}
		}
	default:
		return nil, fmt.Errorf("attack invalido: %q", attack)
	}
	return cases, nil
}
