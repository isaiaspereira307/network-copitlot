// Package proxy implementa o MITM HTTP/HTTPS.
//
// v2.0 (este plano): apenas a assinatura e o glue de store.
// A implementacao completa (goproxy, CA, hooks on_request/on_response)
// vive em plano v1 separado; ver PRD §4.1.
package proxy

import (
	"context"
	"errors"

	"github.com/isaias/network-copitlot/internal/store"
)

type Proxy struct {
	store store.Store
}

func New(s store.Store) *Proxy {
	return &Proxy{store: s}
}

// Start e um stub na v2.0. Retorna ErrNotImplemented ate o plano v1
// entregar a implementacao completa.
func (p *Proxy) Start(ctx context.Context, addr string) error {
	return errors.New("proxy.Start: not implemented (planned for v1 plan)")
}
