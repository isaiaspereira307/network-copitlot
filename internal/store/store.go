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
	// flags de captura (task 17): body pulado por content-type / truncado no cap.
	RespBodySkipped   bool
	RespBodyTruncated bool
	TTFBms            int
	Tags              []string
	Notes             string
}

// ListFilter limita/filtra resultados. Zero = sem limite/filtro.
type ListFilter struct {
	Limit        int
	Offset       int
	MethodFilter string
	StatusFilter int // 0 = any
	HostFilter   string
	PathContains string
	SinceID      int64
	Interesting  bool // so marcados (P1)
}

// RequestSummary e a visao enxuta de um request para listagens.
type RequestSummary struct {
	ID      int64
	Ts      int64
	Method  string
	URL     string
	Status  int
	RespLen int
}

// RequestDetail e a visao completa com corpos (truncaveis).
type RequestDetail struct {
	ID                int64
	Ts                int64
	Method            string
	URL               string
	ReqHeaders        map[string][]string
	ReqBody           []byte
	Status            int
	RespHeaders       map[string][]string
	RespBody          []byte
	RespLen           int
	ReqBodyTruncated  bool
	RespBodyTruncated bool
	ReqTotalLen       int
	RespTotalLen      int
}

// BodyMatch e um hit da busca em corpos.
type BodyMatch struct {
	ID           int64
	URL          string
	MatchSnippet string
}

// ReplayOverrides sao as modificacoes opcionais do replay.
type ReplayOverrides struct {
	HeaderOverrides map[string]string
	BodyOverride    []byte
	MethodOverride  string
	URLOverride     string
	FollowRedirects bool
}

// ReplayResult resume o replay executado.
type ReplayResult struct {
	NewRequestID int64
	Status       int
	RespLen      int
}

// Endpoint agrega requests por metodo+path normalizado.
type Endpoint struct {
	Method    string
	Path      string
	HitCount  int
	SampleIDs []int64
}

// Diff compara dois requests/responses.
type Diff struct {
	Mode      string
	Lines     []string
	ChangedAB []string
	ChangedBA []string
}

// Store persiste Request. Implementacoes: SQLite (per-target), futuro: memoria.
type Store interface {
	Insert(r *Request) (int64, error)
	List(f ListFilter) ([]*RequestSummary, error)
	Get(id int64) (*Request, error)
	GetDetail(id int64, include string, maxBody int, bodyRange string) (*RequestDetail, error)
	SearchBodies(pattern string, scope string, limit int) ([]*BodyMatch, error)
	Replay(id int64, overrides ReplayOverrides, scopeMatch func(string) bool) (*ReplayResult, error)
	ListEndpoints() ([]*Endpoint, error)
	DiffRequests(a, b int64, mode string) (*Diff, error)
	// All faz stream de todo request capturado COM corpos (req+resp), para scanner/export.
	All() ([]*Request, error)
	Count() (int, error)
	Close() error
}
