const rows = document.querySelector('#edge-rows');
const summary = document.querySelector('#summary');
const notice = document.querySelector('#notice');
const edgeDialog = document.querySelector('#edge-dialog');
const installDialog = document.querySelector('#install-dialog');
const form = document.querySelector('#edge-form');
let edges = [];

const text = (value) => value == null ? '' : String(value);

async function request(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || `请求失败（HTTP ${response.status}）`);
  return payload;
}

function showNotice(message, error = false) {
  notice.hidden = !message;
  notice.textContent = message;
  notice.classList.toggle('error', error);
}

function render() {
  summary.textContent = `${edges.length} 个节点，${edges.filter((edge) => edge.runtime?.online).length} 个在线`;
  if (!edges.length) {
    rows.innerHTML = '<tr><td colspan="8" class="empty">尚未创建 Edge</td></tr>';
    return;
  }
  rows.replaceChildren(...edges.map((edge) => {
    const row = document.createElement('tr');
    const config = edge.config || {};
    const runtime = edge.runtime || {};
    const identity = document.createElement('td');
    const name = document.createElement('strong'); name.textContent = text(config.name);
    const id = document.createElement('small'); id.textContent = text(config.edge_id);
    identity.append(name, id); row.append(identity);
    [config.region, config.public_endpoint, config.capacity].forEach((value) => { const cell = document.createElement('td'); cell.textContent = text(value); row.append(cell); });
    const statusCell = document.createElement('td');
    const status = document.createElement('span'); status.className = `status ${runtime.online ? 'online' : 'offline'}`; status.textContent = runtime.online ? '在线' : '离线';
    statusCell.append(status); row.append(statusCell);
    [runtime.online ? runtime.software_version : '未连接', runtime.online ? `${text(runtime.agent_count || 0)} daemon / ${text(runtime.session_count || 0)} 客户端` : '无'].forEach((value) => { const cell = document.createElement('td'); cell.textContent = text(value); row.append(cell); });
    const action = document.createElement('td');
    const button = document.createElement('button');
    button.className = 'link-button'; button.textContent = '编辑'; button.addEventListener('click', () => openEdit(edge));
    action.append(button); row.append(action);
    return row;
  }));
}

async function load({ quiet = false } = {}) {
  if (!quiet && !edges.length) rows.innerHTML = '<tr><td colspan="8" class="empty">正在加载...</td></tr>';
  try {
    const payload = await request('/api/operator/edges');
    edges = payload.edges || [];
    render(); showNotice('');
  } catch (error) { showNotice(error.message, true); }
}

function openCreate() {
  form.reset(); document.querySelector('#edge-id').value = ''; document.querySelector('#edge-revision').value = '';
  document.querySelector('#dialog-title').textContent = '添加 Edge'; document.querySelector('#enabled-row').hidden = true; edgeDialog.showModal();
}

function openEdit(edge) {
  const config = edge.config;
  document.querySelector('#edge-id').value = config.edge_id;
  document.querySelector('#edge-revision').value = edge.config_revision;
  document.querySelector('#name').value = config.name;
  document.querySelector('#region').value = config.region;
  document.querySelector('#endpoint').value = config.public_endpoint;
  document.querySelector('#capacity').value = config.capacity;
  document.querySelector('#enabled').checked = config.enabled;
  document.querySelector('#dialog-title').textContent = '编辑 Edge'; document.querySelector('#enabled-row').hidden = false; edgeDialog.showModal();
}

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  const save = document.querySelector('#save');
  save.disabled = true; save.textContent = '正在保存...';
  const edgeId = document.querySelector('#edge-id').value;
  const body = { name: document.querySelector('#name').value, region: document.querySelector('#region').value, public_endpoint: document.querySelector('#endpoint').value, capacity: document.querySelector('#capacity').value };
  try {
    let installCommand = '';
    if (edgeId) {
      body.edge_id = edgeId; body.expected_revision = document.querySelector('#edge-revision').value; body.enabled = document.querySelector('#enabled').checked;
      await request(`/api/operator/edges/${edgeId}`, { method: 'PUT', body: JSON.stringify(body) });
    } else {
      const created = await request('/api/operator/edges', { method: 'POST', body: JSON.stringify(body) });
      installCommand = created.install_command;
    }
    edgeDialog.close();
    if (installCommand) { document.querySelector('#install-command').textContent = installCommand; installDialog.showModal(); }
    await load({ quiet: true });
  } catch (error) { showNotice(error.message, true); }
  finally { save.disabled = false; save.textContent = '保存'; }
});

document.querySelector('#create').addEventListener('click', openCreate);
document.querySelector('#refresh').addEventListener('click', () => load({ quiet: true }));
document.querySelector('#copy-command').addEventListener('click', async () => { await navigator.clipboard.writeText(document.querySelector('#install-command').textContent); });
document.querySelectorAll('[data-close]').forEach((button) => button.addEventListener('click', () => document.querySelector(`#${button.dataset.close}`).close()));
load();
