package tui

type TerminalStore struct {
	items map[string]*Terminal
}

func NewTerminalStore() *TerminalStore {
	return &TerminalStore{items: make(map[string]*Terminal)}
}

func (s *TerminalStore) Get(id string) *Terminal {
	if s == nil || id == "" {
		return nil
	}
	return s.items[id]
}

func (s *TerminalStore) GetOrCreate(id string) *Terminal {
	if s == nil || id == "" {
		return nil
	}
	if terminal := s.items[id]; terminal != nil {
		return terminal
	}
	terminal := &Terminal{ID: id}
	s.items[id] = terminal
	return terminal
}

func (s *TerminalStore) Delete(id string) {
	if s == nil || id == "" {
		return
	}
	delete(s.items, id)
}

func (s *TerminalStore) List() []*Terminal {
	if s == nil {
		return nil
	}
	items := make([]*Terminal, 0, len(s.items))
	for _, terminal := range s.items {
		if terminal != nil {
			items = append(items, terminal)
		}
	}
	return items
}
