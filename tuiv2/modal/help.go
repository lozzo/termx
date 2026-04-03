package modal

import "github.com/lozzow/termx/tuiv2/input"

type HelpState struct {
	Sections []HelpSection
}

type HelpSection struct {
	Title    string
	Bindings []HelpBinding
}

type HelpBinding struct {
	Key    string
	Action string
}

func DefaultHelp() *HelpState {
	sections := input.HelpSections()
	out := &HelpState{Sections: make([]HelpSection, 0, len(sections))}
	for _, section := range sections {
		bindings := make([]HelpBinding, 0, len(section.Bindings))
		for _, binding := range section.Bindings {
			bindings = append(bindings, HelpBinding{Key: binding.Key, Action: binding.Action})
		}
		out.Sections = append(out.Sections, HelpSection{Title: section.Title, Bindings: bindings})
	}
	return out
}
