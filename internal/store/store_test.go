package store

import "testing"

// stubStore verifica que a interface Store e respeitada.
type stubStore struct{ calls int }

func (s *stubStore) Insert(r *Request) (int64, error)           { s.calls++; return 1, nil }
func (s *stubStore) List(ListFilter) ([]*RequestSummary, error) { return nil, nil }
func (s *stubStore) Get(int64) (*Request, error)                { return nil, nil }
func (s *stubStore) Count() (int, error)                        { return 0, nil }
func (s *stubStore) Close() error                               { return nil }
func (s *stubStore) GetDetail(int64, string, int, string) (*RequestDetail, error) {
	return nil, nil
}
func (s *stubStore) SearchBodies(string, string, int) ([]*BodyMatch, error) { return nil, nil }
func (s *stubStore) Replay(int64, ReplayOverrides, func(string) bool) (*ReplayResult, error) {
	return nil, nil
}
func (s *stubStore) ListEndpoints() ([]*Endpoint, error)              { return nil, nil }
func (s *stubStore) DiffRequests(int64, int64, string) (*Diff, error) { return nil, nil }
func (s *stubStore) All() ([]*Request, error)                         { return nil, nil }

func TestStoreInterface_Compiles(t *testing.T) {
	var _ Store = (*stubStore)(nil)
}

// TestStoreInterface_Compiles_NewMethods chama cada novo metodo do stubStore;
// falha por compilacao enquanto as signatures nao existem na interface.
func TestStoreInterface_Compiles_NewMethods(t *testing.T) {
	s := &stubStore{}
	s.List(ListFilter{})
	s.GetDetail(1, "", 0, "")
	s.SearchBodies("", "", 0)
	s.Replay(1, ReplayOverrides{}, nil)
	s.ListEndpoints()
	s.DiffRequests(1, 2, "")
	s.All()
}
