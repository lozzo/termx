package core

import (
	"reflect"
	"testing"
)

func TestLiveScreenChangeLogCarriesReplacementThroughLaterCopy(t *testing.T) {
	var log liveScreenChangeLog
	log.append(liveScreenChange{BaseRevision: 0, Revision: 1, ReplacedRows: []int{1}})
	log.append(liveScreenChange{
		BaseRevision: 1,
		Revision:     2,
		RowCopies:    []NativeScreenRowCopy{{SourceRow: 1, DestinationRow: 0, Count: 1}},
	})

	copies, replacements, ok := log.compose(0, 2, 3)
	if !ok {
		t.Fatal("expected contiguous revisions to compose")
	}
	if len(copies) != 0 || !reflect.DeepEqual(replacements, []int{0, 1}) {
		t.Fatalf("copied replacement must still be sent from latest screen, copies=%#v replacements=%#v", copies, replacements)
	}
}

func TestLiveScreenChangeLogEvictsOldRevisionBases(t *testing.T) {
	var log liveScreenChangeLog
	floor := LiveRevision(0)
	for revision := LiveRevision(1); revision <= liveScreenChangeLogMaxRevisions+1; revision++ {
		if nextFloor := log.append(liveScreenChange{BaseRevision: revision - 1, Revision: revision}); nextFloor > floor {
			floor = nextFloor
		}
	}
	if floor != 1 {
		t.Fatalf("oldest delta base floor = %d, want 1", floor)
	}
	if _, _, ok := log.compose(0, liveScreenChangeLogMaxRevisions+1, 3); ok {
		t.Fatal("evicted revision base must require a full screen")
	}
	if _, _, ok := log.compose(1, liveScreenChangeLogMaxRevisions+1, 3); !ok {
		t.Fatal("oldest retained revision base should remain composable")
	}
}
