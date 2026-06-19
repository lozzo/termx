package render

func appendHeaderHitRegions(out []HitRegion, header HeaderVM, rect Rect, viewport Rect) []HitRegion {
	if rect.W <= 0 || rect.H <= 0 || !header.Visible {
		return out
	}
	x := rect.X
	for _, segment := range headerLeftSegments(header) {
		width := DisplayWidth(segment.text)
		if width <= 0 {
			continue
		}
		if segment.actionID != "" {
			out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: Rect{X: x, Y: rect.Y, W: width, H: rect.H}, ActionID: segment.actionID, PaneID: segment.targetID}, viewport)
		}
		x += width
		if x >= rect.X+rect.W {
			break
		}
	}
	return out
}

func appendFooterHitRegions(out []HitRegion, footer FooterVM, rect Rect, frame Rect, viewport Rect) []HitRegion {
	if rect.W <= 0 || rect.H <= 0 || !footer.Visible {
		return out
	}
	_ = frame
	left := footerLeftSegments(footer, rect.W)
	right := footerMetadataSegments(footer, footerHintIsCritical(footer))
	if remaining := rect.W - barSegmentsWidth(left); remaining > 0 {
		right = trimBarSegments(right, remaining)
	} else {
		right = nil
	}
	flush := func(action *string, region *Rect) {
		if *action != "" && region.W > 0 {
			out = appendRegion(out, HitRegion{Kind: HitRegionContentAction, Rect: *region, ActionID: *action}, viewport)
		}
		*action = ""
		*region = Rect{}
	}
	appendSegments := func(segments []barSegment, startX int) {
		x := startX
		currentAction := ""
		currentRect := Rect{}
		for _, segment := range segments {
			width := DisplayWidth(segment.text)
			if width <= 0 {
				continue
			}
			if segment.actionID == "" {
				flush(&currentAction, &currentRect)
				x += width
				continue
			}
			if currentAction == segment.actionID && currentRect.X+currentRect.W == x {
				currentRect.W += width
			} else {
				flush(&currentAction, &currentRect)
				currentAction = segment.actionID
				currentRect = Rect{X: x, Y: rect.Y, W: width, H: 1}
			}
			x += width
			if x >= rect.X+rect.W {
				break
			}
		}
		flush(&currentAction, &currentRect)
	}
	appendSegments(left, rect.X)
	if len(right) > 0 {
		appendSegments(right, rect.X+maxInt(0, rect.W-barSegmentsWidth(right)))
	}
	return out
}
