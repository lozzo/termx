package sessionstate

import (
	"github.com/lozzow/termx/tuiv2/sessiondoc"
	"github.com/lozzow/termx/tuiv2/workbench"
	"github.com/lozzow/termx/tuiv2/workbenchcodec"
)

func ExportWorkbench(wb *workbench.Workbench) *sessiondoc.Doc {
	return workbenchcodec.ExportWorkbench(wb)
}

func ImportDoc(doc *sessiondoc.Doc) *workbench.Workbench {
	return workbenchcodec.ImportDoc(doc)
}

func PaneTerminalBindings(doc *sessiondoc.Doc) map[string]string {
	return workbenchcodec.PaneTerminalBindings(doc)
}
