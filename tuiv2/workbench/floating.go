package workbench

func removeFloatingPane(entries []*FloatingState, paneID string) []*FloatingState {
	if len(entries) == 0 || paneID == "" {
		return entries
	}
	kept := entries[:0]
	for _, entry := range entries {
		if entry == nil || entry.PaneID == paneID {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
