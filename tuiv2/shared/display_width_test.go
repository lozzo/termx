package shared

import "testing"

func TestIsEastAsianAmbiguousWidthCluster(t *testing.T) {
	if !IsEastAsianAmbiguousWidthCluster("§") {
		t.Fatal("expected section sign to be treated as East Asian ambiguous width")
	}
	if IsEastAsianAmbiguousWidthCluster("A") {
		t.Fatal("expected plain ASCII not to be treated as East Asian ambiguous width")
	}
}

func TestIsHostWidthAmbiguousCluster(t *testing.T) {
	if !IsHostWidthAmbiguousCluster("♻️", 2) {
		t.Fatal("expected FE0F emoji variation sequence to be treated as host-width ambiguous")
	}
	if !IsHostWidthAmbiguousCluster("é", 1) {
		t.Fatal("expected East Asian ambiguous-width text to be treated as host-width ambiguous")
	}
	if IsHostWidthAmbiguousCluster("ok", 2) {
		t.Fatal("expected ordinary ASCII text not to be treated as host-width ambiguous")
	}
}
