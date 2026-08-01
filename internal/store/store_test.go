package store

import "testing"

// stubStore verifica que a interface Store e respeitada.
type stubStore struct{ calls int }

func (s *stubStore) Insert(r *Request) (int64, error) { s.calls++; return 1, nil }
func (s *stubStore) List(ListFilter) ([]*Request, error) { return nil, nil }
func (s *stubStore) Get(int64) (*Request, error)       { return nil, nil }
func (s *stubStore) Count() (int, error)              { return 0, nil }
func (s *stubStore) Close() error                      { return nil }

func TestStoreInterface_Compiles(t *testing.T) {
	var _ Store = (*stubStore)(nil)
}
