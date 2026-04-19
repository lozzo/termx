package render

type bodyRenderCache struct {
	width  int
	height int
	canvas *composedCanvas
}

func newBodyRenderCache(_ *bodyRenderCache, canvas *composedCanvas, _ []paneRenderEntry, width, height int) *bodyRenderCache {
	return &bodyRenderCache{
		width:  width,
		height: height,
		canvas: canvas,
	}
}
