package render

import "testing"

func TestComposeRenderMetadataWrapsBodyOwnerMapWithChromeRows(t *testing.T) {
	body := &PresentMetadata{
		OwnerMap: [][]uint32{
			{10, 10, 20, 20},
			{10, 10, 20, 20},
		},
	}
	meta := composeRenderMetadata(4, 4, false, body)
	if meta == nil {
		t.Fatal("expected composed render metadata")
	}
	if got := meta.OwnerMap[0][0]; got != renderOwnerTopChrome {
		t.Fatalf("expected top chrome owner on first row, got %d", got)
	}
	if got := meta.OwnerMap[1][2]; got != 20 {
		t.Fatalf("expected body owner copied into frame metadata, got %d", got)
	}
	if got := meta.OwnerMap[3][3]; got != renderOwnerBottomChrome {
		t.Fatalf("expected bottom chrome owner on final row, got %d", got)
	}
}
