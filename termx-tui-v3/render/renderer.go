package render

type Renderer struct {
	Theme Theme
}

func NewRenderer(theme Theme) Renderer {
	if theme == (Theme{}) {
		theme = DefaultTheme()
	}
	return Renderer{Theme: theme}
}

func (renderer Renderer) RenderResult(vm RenderVM) RenderResult {
	if vm.Theme != (Theme{}) {
		renderer.Theme = vm.Theme
	}
	return renderer.renderFramework(vm)
}

func (renderer Renderer) Render(vm RenderVM) Frame {
	return renderer.RenderResult(vm).Frame()
}
