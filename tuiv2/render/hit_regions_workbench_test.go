package render

import (
	"fmt"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/lozzow/termx/tuiv2/workbench"
)

func makeTabBarState(width int, tabs []string) VisibleRenderState {
	visibleTabs := make([]workbench.VisibleTab, 0, len(tabs))
	for index, name := range tabs {
		visibleTabs = append(visibleTabs, workbench.VisibleTab{
			ID:   fmt.Sprintf("tab-%d", index+1),
			Name: name,
		})
	}
	return VisibleRenderState{
		Workbench: &workbench.VisibleWorkbench{
			WorkspaceName: "main",
			ActiveTab:     0,
			Tabs:          visibleTabs,
		},
		TermSize: TermSize{Width: width, Height: 20},
	}
}

func makeTabBarVM(width int, tabs []string) RenderVM {
	visibleTabs := make([]workbench.VisibleTab, 0, len(tabs))
	for index, name := range tabs {
		visibleTabs = append(visibleTabs, workbench.VisibleTab{
			ID:   fmt.Sprintf("tab-%d", index+1),
			Name: name,
		})
	}
	return RenderVM{
		Workbench: &workbench.VisibleWorkbench{
			WorkspaceName: "main",
			ActiveTab:     0,
			Tabs:          visibleTabs,
		},
		TermSize: TermSize{Width: width, Height: 20},
	}
}

func TestRenderTabBarIncludesCloseAndCreateAffordances(t *testing.T) {
	state := makeTabBarState(120, []string{"build", "logs"})

	line := xansi.Strip(renderTabBar(state))
	if !strings.Contains(line, "1 build") || !strings.Contains(line, "2 logs") {
		t.Fatalf("expected tab switch affordances in tab bar, got %q", line)
	}
	if strings.Count(line, paneCloseIcon()) < 2 {
		t.Fatalf("expected close affordance for each tab, got %q", line)
	}
	if !strings.Contains(line, "[+]") {
		t.Fatalf("expected create-tab affordance, got %q", line)
	}
}

func TestTabBarHitRegionsExposeStableTopBarTargets(t *testing.T) {
	regions := TabBarHitRegions(makeTabBarVM(120, []string{"build", "logs"}))
	if len(regions) == 0 {
		t.Fatal("expected tab bar hit regions")
	}

	counts := map[HitRegionKind]int{}
	closeWidth := 0
	for _, region := range regions {
		counts[region.Kind]++
		if region.Kind == HitRegionTabClose {
			if closeWidth == 0 {
				closeWidth = region.Rect.W
			} else if region.Rect.W != closeWidth {
				t.Fatalf("expected stable close-slot width, got %d and %d", closeWidth, region.Rect.W)
			}
		}
	}

	if counts[HitRegionWorkspaceLabel] != 1 {
		t.Fatalf("expected 1 workspace region, got %d (%#v)", counts[HitRegionWorkspaceLabel], regions)
	}
	if counts[HitRegionTabSwitch] != 2 {
		t.Fatalf("expected 2 tab-switch regions, got %d (%#v)", counts[HitRegionTabSwitch], regions)
	}
	if counts[HitRegionTabClose] != 2 {
		t.Fatalf("expected 2 tab-close regions, got %d (%#v)", counts[HitRegionTabClose], regions)
	}
	if counts[HitRegionTabCreate] != 1 {
		t.Fatalf("expected 1 tab-create region, got %d (%#v)", counts[HitRegionTabCreate], regions)
	}
}

func TestTabBarHitRegionsDropCreateWhenWidthIsTight(t *testing.T) {
	state := makeTabBarState(21, []string{"a"})
	regions := TabBarHitRegions(makeTabBarVM(21, []string{"a"}))
	line := xansi.Strip(renderTabBar(state))

	for _, region := range regions {
		if region.Rect.X+region.Rect.W > state.TermSize.Width {
			t.Fatalf("region exceeds tab bar width: %#v", region)
		}
		if region.Kind == HitRegionTabCreate {
			t.Fatalf("expected create region to be omitted in tight width, got %#v", region)
		}
	}
	if strings.Contains(line, "[+]") {
		t.Fatalf("expected create affordance omitted in tight width, got %q", line)
	}
}

func TestTabBarRetainsWorkspaceAndCreateAffordanceWhenWorkspaceHasNoTabs(t *testing.T) {
	state := makeTabBarState(120, nil)

	line := xansi.Strip(renderTabBar(state))
	if !strings.Contains(line, "main") {
		t.Fatalf("expected empty workspace tab bar to keep workspace label, got %q", line)
	}
	if !strings.Contains(line, "[+]") {
		t.Fatalf("expected empty workspace tab bar to keep create affordance, got %q", line)
	}

	regions := TabBarHitRegions(makeTabBarVM(120, nil))
	counts := map[HitRegionKind]int{}
	for _, region := range regions {
		counts[region.Kind]++
	}
	if counts[HitRegionWorkspaceLabel] != 1 || counts[HitRegionTabCreate] != 1 {
		t.Fatalf("expected workspace label and tab-create regions for empty workspace, got %#v", regions)
	}
	if counts[HitRegionTabSwitch] != 0 || counts[HitRegionTabClose] != 0 {
		t.Fatalf("expected no tab regions when workspace has no tabs, got %#v", regions)
	}
}

func TestTabBarOmitsWorkspaceAndTabManagementActions(t *testing.T) {
	state := makeTabBarState(200, []string{"build", "logs"})
	regions := TabBarHitRegions(makeTabBarVM(200, []string{"build", "logs"}))
	line := xansi.Strip(renderTabBar(state))

	disallowedKinds := []HitRegionKind{
		HitRegionTabRename,
		HitRegionTabKill,
		HitRegionWorkspacePrev,
		HitRegionWorkspaceNext,
		HitRegionWorkspaceCreate,
		HitRegionWorkspaceRename,
		HitRegionWorkspaceDelete,
	}
	for _, kind := range disallowedKinds {
		for _, region := range regions {
			if region.Kind == kind {
				t.Fatalf("expected management region %q to be omitted, got %#v", kind, regions)
			}
		}
	}
	for _, token := range []string{"[tr]", "[tx]", "[w<]", "[w>]", "[w+]", "[wr]", "[wx]"} {
		if strings.Contains(line, token) {
			t.Fatalf("expected tab bar to omit %q, got %q", token, line)
		}
	}
}

func TestTabBarHitRegionsKeepOnlyCoreNavigation(t *testing.T) {
	wideRegions := TabBarHitRegions(makeTabBarVM(120, []string{"a"}))
	wideHasCreate := false
	for _, region := range wideRegions {
		if region.Kind == HitRegionTabCreate {
			wideHasCreate = true
		}
	}
	if !wideHasCreate {
		t.Fatalf("expected baseline width to contain create action, got %#v", wideRegions)
	}

	tightRegions := TabBarHitRegions(makeTabBarVM(26, []string{"a"}))
	coreCounts := map[HitRegionKind]int{}
	for _, region := range tightRegions {
		coreCounts[region.Kind]++
		switch region.Kind {
		case HitRegionTabRename, HitRegionTabKill, HitRegionWorkspacePrev, HitRegionWorkspaceNext, HitRegionWorkspaceCreate, HitRegionWorkspaceRename, HitRegionWorkspaceDelete:
			t.Fatalf("expected management actions to be dropped before core nav in tight width, got %#v", tightRegions)
		}
	}
	if coreCounts[HitRegionWorkspaceLabel] != 1 || coreCounts[HitRegionTabSwitch] != 1 || coreCounts[HitRegionTabClose] != 1 || coreCounts[HitRegionTabCreate] != 1 {
		t.Fatalf("expected tight width to preserve core top-nav regions, got %#v", tightRegions)
	}
}
