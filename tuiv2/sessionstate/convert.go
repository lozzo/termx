package sessionstate

import (
	"github.com/lozzow/termx/internal/workbenchcodec"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/lozzow/termx/workbenchdoc"
)

func ExportWorkbench(wb *workbench.Workbench) *workbenchdoc.Doc {
	return workbenchcodec.ExportWorkbench(wb)
}

func ImportDoc(doc *workbenchdoc.Doc) *workbench.Workbench {
	return workbenchcodec.ImportDoc(doc)
}

func PaneTerminalBindings(doc *workbenchdoc.Doc) map[string]string {
	return workbenchcodec.PaneTerminalBindings(doc)
}
