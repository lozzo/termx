import re

with open('remote-ui/src/WebControlRemoteApp.tsx', 'r') as f:
    content = f.read()

# Replace variables in SettingsView
replacements = {
    'bg-[var(--termx-bg)] text-[var(--termx-text)]': 'bg-zinc-50 text-zinc-950',
    'border-[var(--termx-border-subtle)] bg-[var(--termx-surface)]': 'border-zinc-200/70 bg-white',
    'bg-[var(--termx-surface-raised)] text-[var(--termx-text)]': 'bg-white text-zinc-700',
    'focus-visible:ring-[var(--termx-accent)]': 'focus-visible:ring-blue-500',
    'font-semibold leading-6">Settings': 'font-semibold leading-6 text-zinc-900">Settings',
    'text-[var(--termx-muted)]': 'text-zinc-500',
    'overflow-y-auto px-4 py-5': 'overflow-y-auto px-4 py-5 pb-[calc(env(safe-area-inset-bottom)+1.5rem)]',
    'border-[var(--termx-border-subtle)]': 'border-zinc-200',
    'bg-[var(--termx-surface-raised)]': 'bg-white',
    'bg-[var(--termx-surface)]': 'bg-white',
    'text-[var(--termx-text)]': 'text-zinc-900',
    'focus:ring-[var(--termx-accent)]': 'focus:ring-blue-500',
    'focus:border-[var(--termx-accent)]': 'focus:border-blue-500',
    'bg-[var(--termx-accent)]': 'bg-blue-600',
    'text-[var(--termx-accent-text)]': 'text-white',
}

# Only replace within SettingsView, SettingsSection, SettingsRow, SettingsSelect to avoid messing up terminal previews
start_idx = content.find('function SettingsView')
if start_idx != -1:
    end_idx = content.find('function ThemePicker')
    if end_idx != -1:
        prefix = content[:start_idx]
        middle = content[start_idx:end_idx]
        suffix = content[end_idx:]
        
        for old, new in replacements.items():
            middle = middle.replace(old, new)
            
        content = prefix + middle + suffix

# Global App Wrapper removes `style={appThemeStyle}` to prevent inheritance
content = re.sub(r'style=\{appThemeStyle\}\s*', '', content)
# Ensure LocalRemoteApp and WebControlRemoteApp do not inherit the terminal background via the wrapper
content = re.sub(r'className="([^"]*)bg-\[var\(--termx-bg\)\]([^"]*)"', r'className="\1bg-zinc-50\2"', content)
# Ensure text color doesn't inherit terminal text in global wrappers if it exists
content = re.sub(r'className="([^"]*)text-\[var\(--termx-text\)\]([^"]*)"', r'className="\1text-zinc-950\2"', content)


with open('remote-ui/src/WebControlRemoteApp.tsx', 'w') as f:
    f.write(content)

with open('remote-ui/src/LocalRemoteApp.tsx', 'r') as f:
    content = f.read()

# Same for LocalRemoteApp
content = re.sub(r'style=\{appThemeStyle\}\s*', '', content)
content = re.sub(r'style=\{terminalThemeStyle\}\s*', '', content) # LocalRemoteApp uses terminalThemeStyle sometimes
content = re.sub(r'className="([^"]*)bg-\[var\(--termx-bg\)\]([^"]*)"', r'className="\1bg-zinc-50\2"', content)
content = re.sub(r'className="([^"]*)text-\[var\(--termx-text\)\]([^"]*)"', r'className="\1text-zinc-950\2"', content)


with open('remote-ui/src/LocalRemoteApp.tsx', 'w') as f:
    f.write(content)

