package render

type Renderer interface {
	Render(any) string
}
