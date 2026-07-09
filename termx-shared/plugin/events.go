package plugin

const (
	SystemEventDaemonTerminalCreated        EventType = "termx.daemon.terminal.created"
	SystemEventDaemonTerminalExited         EventType = "termx.daemon.terminal.exited"
	SystemEventDaemonTerminalRemoved        EventType = "termx.daemon.terminal.removed"
	SystemEventDaemonTerminalResized        EventType = "termx.daemon.terminal.resized"
	SystemEventDaemonTerminalOutputActivity EventType = "termx.daemon.terminal.output_activity"
	SystemEventDaemonTerminalOutputIdle     EventType = "termx.daemon.terminal.output_idle"
	SystemEventDaemonTerminalOutputResumed  EventType = "termx.daemon.terminal.output_resumed"

	SystemEventClientPanelCreated EventType = "termx.client.panel.created"
	SystemEventClientPanelClosed  EventType = "termx.client.panel.closed"
	SystemEventClientPanelBound   EventType = "termx.client.panel.bound"
	SystemEventClientPanelResized EventType = "termx.client.panel.resized"
	SystemEventClientPanelFocused EventType = "termx.client.panel.focused"

	SystemEventClientFloatCreated EventType = "termx.client.float.created"
	SystemEventClientFloatClosed  EventType = "termx.client.float.closed"
	SystemEventClientFloatResized EventType = "termx.client.float.resized"
	SystemEventClientFloatFocused EventType = "termx.client.float.focused"

	SystemEventClientTabCreated   EventType = "termx.client.tab.created"
	SystemEventClientTabActivated EventType = "termx.client.tab.activated"

	ObjectKindTerminal = "terminal"
	ObjectKindPanel    = "panel"
	ObjectKindFloat    = "float"
	ObjectKindTab      = "tab"

	CapabilityTerminalLifecycleRead Capability = "terminal.lifecycle.read"
	CapabilityTerminalActivityRead  Capability = "terminal.activity.read"
	CapabilityTerminalSizeRead      Capability = "terminal.size.read"
	CapabilityClientPanelRead       Capability = "client.panel.read"
	CapabilityClientFloatRead       Capability = "client.float.read"
	CapabilityClientTabRead         Capability = "client.tab.read"
)

// DefaultSystemEventCatalog 返回第一阶段内建 hook event 目录。
// 目录是真值 owner 和默认投递策略的共享事实；manifest 只能订阅这些 termx.* 事件，runner 不能自报或扩展系统事件。
func DefaultSystemEventCatalog() []EventSpec {
	return []EventSpec{
		{
			Type:            SystemEventDaemonTerminalCreated,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityTerminalLifecycleRead},
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventDaemonTerminalExited,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityTerminalLifecycleRead},
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventDaemonTerminalRemoved,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityTerminalLifecycleRead},
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventDaemonTerminalResized,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityTerminalSizeRead},
			Lossy:           true,
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventDaemonTerminalOutputActivity,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityTerminalActivityRead},
			Lossy:           true,
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventDaemonTerminalOutputIdle,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityTerminalActivityRead},
			Lossy:           true,
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventDaemonTerminalOutputResumed,
			SourceHost:      HostDaemon,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityTerminalActivityRead},
			Lossy:           true,
			ObjectKind:      ObjectKindTerminal,
		},
		{
			Type:            SystemEventClientPanelCreated,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientPanelRead},
			ObjectKind:      ObjectKindPanel,
		},
		{
			Type:            SystemEventClientPanelClosed,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientPanelRead},
			ObjectKind:      ObjectKindPanel,
		},
		{
			Type:            SystemEventClientPanelBound,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientPanelRead},
			ObjectKind:      ObjectKindPanel,
		},
		{
			Type:            SystemEventClientPanelResized,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityClientPanelRead},
			Lossy:           true,
			ObjectKind:      ObjectKindPanel,
		},
		{
			Type:            SystemEventClientPanelFocused,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityClientPanelRead},
			Lossy:           true,
			ObjectKind:      ObjectKindPanel,
		},
		{
			Type:            SystemEventClientFloatCreated,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientFloatRead},
			ObjectKind:      ObjectKindFloat,
		},
		{
			Type:            SystemEventClientFloatClosed,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientFloatRead},
			ObjectKind:      ObjectKindFloat,
		},
		{
			Type:            SystemEventClientFloatResized,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityClientFloatRead},
			Lossy:           true,
			ObjectKind:      ObjectKindFloat,
		},
		{
			Type:            SystemEventClientFloatFocused,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryCoalesced},
			RequiredCaps:    []Capability{CapabilityClientFloatRead},
			Lossy:           true,
			ObjectKind:      ObjectKindFloat,
		},
		{
			Type:            SystemEventClientTabCreated,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientTabRead},
			ObjectKind:      ObjectKindTab,
		},
		{
			Type:            SystemEventClientTabActivated,
			SourceHost:      HostClient,
			DefaultDelivery: HookDelivery{Mode: DeliveryQueued},
			RequiredCaps:    []Capability{CapabilityClientTabRead},
			ObjectKind:      ObjectKindTab,
		},
	}
}
