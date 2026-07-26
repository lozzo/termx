const state = { edges: null, daemons: null };
const text = (value) => value == null ? '' : String(value);
const edgeRows = document.querySelector('#edge-rows');
const daemonRows = document.querySelector('#daemon-rows');
const edgeDialog = document.querySelector('#edge-dialog');
const daemonDialog = document.querySelector('#daemon-dialog');
const commandDialog = document.querySelector('#command-dialog');

async function request(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || `请求失败（HTTP ${response.status}）`);
  return payload;
}

function notice(module, message, error = false) {
  const target = document.querySelector(`#${module}-notice`);
  target.hidden = !message; target.textContent = message; target.classList.toggle('error', error);
}

function currentModule() { return location.pathname === '/daemons' ? 'daemons' : 'edges'; }

function showModule(module, push = false) {
  document.querySelectorAll('[data-view]').forEach((view) => { view.hidden = view.dataset.view !== module; });
  document.querySelectorAll('[data-module]').forEach((link) => link.classList.toggle('active', link.dataset.module === module));
  if (push) history.pushState({}, '', module === 'daemons' ? '/daemons' : '/edges');
  if (module === 'daemons') loadDaemons(); else loadEdges();
}

document.querySelectorAll('[data-module]').forEach((link) => link.addEventListener('click', (event) => {
  event.preventDefault(); showModule(link.dataset.module, true);
}));
addEventListener('popstate', () => showModule(currentModule()));

function renderEdges() {
  const edges = state.edges || [];
  document.querySelector('#edge-summary').textContent = `${edges.length} 个节点，${edges.filter((edge) => edge.runtime?.online).length} 个在线`;
  if (!edges.length) { edgeRows.innerHTML = '<tr><td colspan="8" class="empty">尚未创建 Edge</td></tr>'; return; }
  edgeRows.replaceChildren(...edges.map((edge) => {
    const row = document.createElement('tr'); const config = edge.config || {}; const runtime = edge.runtime || {};
    const identity = document.createElement('td'); const name = document.createElement('strong'); name.textContent = text(config.name); const id = document.createElement('small'); id.textContent = text(config.edge_id); identity.append(name, id); row.append(identity);
    [config.region, config.public_endpoint, config.capacity].forEach((value) => { const cell = document.createElement('td'); cell.textContent = text(value); row.append(cell); });
    row.append(statusCell(runtime.online, runtime.online ? '在线' : '离线'));
    [runtime.online ? runtime.software_version : '未连接', runtime.online ? `${text(runtime.agent_count || 0)} daemon / ${text(runtime.session_count || 0)} 客户端` : '无'].forEach((value) => { const cell = document.createElement('td'); cell.textContent = text(value); row.append(cell); });
    const action = document.createElement('td'); const button = document.createElement('button'); button.className = 'link-button'; button.textContent = '编辑'; button.addEventListener('click', () => openEdgeEdit(edge)); action.append(button); row.append(action); return row;
  }));
}

function renderDaemons() {
  const daemons = state.daemons || [];
  document.querySelector('#daemon-summary').textContent = `${daemons.length} 个 Daemon，${daemons.filter((item) => item.runtime?.online).length} 个在线`;
  if (!daemons.length) { daemonRows.innerHTML = '<tr><td colspan="7" class="empty">尚未注册 Daemon</td></tr>'; return; }
  daemonRows.replaceChildren(...daemons.map((item) => {
    const row = document.createElement('tr'); const daemon = item.daemon || {}; const runtime = item.runtime || {};
    row.append(identityCell(daemon.display_name, daemon.daemon_id), identityCell(daemon.account_name, daemon.account_id), identityCell(daemon.device_id, daemon.device_fingerprint));
    row.append(statusCell(runtime.online, runtime.online ? '在线' : (daemon.revoked ? '已撤销' : '离线')));
    row.append(identityCell(runtime.online ? runtime.edge_name : '无', runtime.online ? `${runtime.edge_public_endpoint} · ${runtime.edge_region} · ${runtime.edge_id}` : '未注册到 Edge'));
    [runtime.online ? runtime.generation : '-', formatTime(daemon.created_at)].forEach((value) => { const cell = document.createElement('td'); cell.textContent = text(value); row.append(cell); });
    return row;
  }));
}

function identityCell(primary, secondary) { const cell = document.createElement('td'); const strong = document.createElement('strong'); strong.textContent = text(primary); const small = document.createElement('small'); small.textContent = text(secondary); cell.append(strong, small); return cell; }
function statusCell(online, label) { const cell = document.createElement('td'); const status = document.createElement('span'); status.className = `status ${online ? 'online' : 'offline'}`; status.textContent = label; cell.append(status); return cell; }
function formatTime(value) { const date = new Date(value); return Number.isNaN(date.valueOf()) ? '-' : date.toLocaleString('zh-CN', { hour12: false }); }

async function loadEdges({ quiet = false } = {}) {
  if (!quiet && state.edges === null) edgeRows.innerHTML = '<tr><td colspan="8" class="empty">正在加载...</td></tr>';
  try { const payload = await request('/api/operator/edges'); state.edges = payload.edges || []; renderEdges(); notice('edge', ''); } catch (error) { notice('edge', error.message, true); }
}
async function loadDaemons({ quiet = false } = {}) {
  if (!quiet && state.daemons === null) daemonRows.innerHTML = '<tr><td colspan="7" class="empty">正在加载...</td></tr>';
  try { const payload = await request('/api/operator/daemons'); state.daemons = payload.daemons || []; renderDaemons(); notice('daemon', ''); } catch (error) { notice('daemon', error.message, true); }
}

function openEdgeCreate() {
  document.querySelector('#edge-form').reset(); document.querySelector('#edge-id').value = ''; document.querySelector('#edge-revision').value = '';
  document.querySelector('#edge-dialog-title').textContent = '添加 Edge'; document.querySelector('#edge-enabled-row').hidden = true; edgeDialog.showModal();
}
function openEdgeEdit(edge) {
  const config = edge.config; document.querySelector('#edge-id').value = config.edge_id; document.querySelector('#edge-revision').value = edge.config_revision;
  document.querySelector('#edge-name').value = config.name; document.querySelector('#edge-region').value = config.region; document.querySelector('#edge-endpoint').value = config.public_endpoint; document.querySelector('#edge-capacity').value = config.capacity; document.querySelector('#edge-enabled').checked = config.enabled;
  document.querySelector('#edge-dialog-title').textContent = '编辑 Edge'; document.querySelector('#edge-enabled-row').hidden = false; edgeDialog.showModal();
}

document.querySelector('#edge-form').addEventListener('submit', async (event) => {
  event.preventDefault(); const save = document.querySelector('#edge-save'); save.disabled = true; save.textContent = '正在保存...';
  const edgeId = document.querySelector('#edge-id').value; const body = { name: document.querySelector('#edge-name').value, region: document.querySelector('#edge-region').value, public_endpoint: document.querySelector('#edge-endpoint').value, capacity: document.querySelector('#edge-capacity').value };
  try {
    let command = '';
    if (edgeId) { body.edge_id = edgeId; body.expected_revision = document.querySelector('#edge-revision').value; body.enabled = document.querySelector('#edge-enabled').checked; await request(`/api/operator/edges/${edgeId}`, { method: 'PUT', body: JSON.stringify(body) }); }
    else { const created = await request('/api/operator/edges', { method: 'POST', body: JSON.stringify(body) }); command = created.install_command; }
    edgeDialog.close(); if (command) showCommand('Edge 安装命令', command); await loadEdges({ quiet: true });
  } catch (error) { notice('edge', error.message, true); } finally { save.disabled = false; save.textContent = '保存'; }
});

document.querySelector('#daemon-form').addEventListener('submit', async (event) => {
  event.preventDefault(); const save = document.querySelector('#daemon-save'); save.disabled = true; save.textContent = '正在生成...';
  const body = { account_id: document.querySelector('#account-id').value, account_name: document.querySelector('#account-name').value, daemon_name: document.querySelector('#daemon-name').value };
  try { const created = await request('/api/operator/daemons', { method: 'POST', body: JSON.stringify(body) }); daemonDialog.close(); showCommand('Daemon 注册命令', created.enroll_command); }
  catch (error) { notice('daemon', error.message, true); } finally { save.disabled = false; save.textContent = '生成'; }
});

function showCommand(title, command) { document.querySelector('#command-title').textContent = title; document.querySelector('#one-time-command').textContent = command; commandDialog.showModal(); }
document.querySelector('#edge-create').addEventListener('click', openEdgeCreate);
document.querySelector('#edge-refresh').addEventListener('click', () => loadEdges({ quiet: true }));
document.querySelector('#daemon-create').addEventListener('click', () => { document.querySelector('#daemon-form').reset(); daemonDialog.showModal(); });
document.querySelector('#daemon-refresh').addEventListener('click', () => loadDaemons({ quiet: true }));
document.querySelector('#copy-command').addEventListener('click', async () => navigator.clipboard.writeText(document.querySelector('#one-time-command').textContent));
document.querySelectorAll('[data-close]').forEach((button) => button.addEventListener('click', () => document.querySelector(`#${button.dataset.close}`).close()));
showModule(currentModule());
