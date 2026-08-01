package store

// Request representa uma transacao HTTP capturada.
type Request struct {
	ID          int64
	Ts          int64 // unix epoch ms
	Method      string
	URL         string
	ReqHeaders  map[string][]string
	ReqBody     []byte
	Status      int
	RespHeaders map[string][]string
	RespBody    []byte
	RespLen     int
	TTFBms      int
	Tags        []string
	Notes       string
}

// ListFilter limita resultados. Zero = sem limite.
type ListFilter struct {
	Limit int
}

// Store persiste Request. Implementacoes: SQLite (per-target), futuro: memoria.
type Store interface {
	Insert(r *Request) (int64, error)
	List(filter ListFilter) ([]*Request, error)
	Get(id int64) (*Request, error)
	Count() (int, error)
	Close() error
}
