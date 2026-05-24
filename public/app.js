const state = {
  items: [],
  filter: '',
  token: readToken(),
};

const elements = {
  pasteZone: document.querySelector('#pasteZone'),
  textInput: document.querySelector('#textInput'),
  fileInput: document.querySelector('#fileInput'),
  sendTextButton: document.querySelector('#sendTextButton'),
  refreshButton: document.querySelector('#refreshButton'),
  searchInput: document.querySelector('#searchInput'),
  items: document.querySelector('#items'),
  itemCount: document.querySelector('#itemCount'),
  serverState: document.querySelector('#serverState'),
  toast: document.querySelector('#toast'),
};

let toastTimer = 0;

init();

function init() {
  elements.refreshButton.addEventListener('click', () => loadItems());
  elements.sendTextButton.addEventListener('click', () => saveManualText());
  elements.fileInput.addEventListener('change', () => saveFiles(elements.fileInput.files));
  elements.searchInput.addEventListener('input', () => {
    state.filter = elements.searchInput.value;
    renderItems();
  });

  elements.pasteZone.addEventListener('dragover', (event) => {
    event.preventDefault();
    elements.pasteZone.classList.add('is-dragging');
  });
  elements.pasteZone.addEventListener('dragleave', () => elements.pasteZone.classList.remove('is-dragging'));
  elements.pasteZone.addEventListener('drop', (event) => {
    event.preventDefault();
    elements.pasteZone.classList.remove('is-dragging');
    saveFiles(event.dataTransfer.files);
  });

  window.addEventListener('paste', handlePaste);
  loadItems();
}

function readToken() {
  const params = new URLSearchParams(window.location.search);
  if (params.has('token')) {
    const token = params.get('token') || '';
    if (token) {
      localStorage.setItem('clipToken', token);
    } else {
      localStorage.removeItem('clipToken');
    }
    window.history.replaceState({}, document.title, window.location.pathname);
    return token;
  }
  return localStorage.getItem('clipToken') || '';
}

async function handlePaste(event) {
  if (!event.clipboardData || event.target === elements.searchInput) {
    return;
  }

  const files = Array.from(event.clipboardData.files || []);
  const text = event.clipboardData.getData('text/plain');
  if (!files.length && !text) {
    return;
  }

  event.preventDefault();
  try {
    if (files.length) {
      await saveFiles(files, { skipReload: true });
    }
    if (text) {
      await createText(text, { skipReload: true });
    }
    elements.textInput.value = '';
    await loadItems();
    showToast('已保存');
  } catch (error) {
    showToast(error.message);
  }
}

async function saveManualText() {
  const text = elements.textInput.value;
  if (!text.trim()) {
    showToast('文本为空');
    return;
  }

  try {
    await createText(text);
    elements.textInput.value = '';
    showToast('已保存');
  } catch (error) {
    showToast(error.message);
  }
}

async function saveFiles(fileList, options = {}) {
  const files = Array.from(fileList || []);
  if (!files.length) {
    return;
  }

  for (const file of files) {
    await uploadFile(file);
  }

  elements.fileInput.value = '';
  if (!options.skipReload) {
    await loadItems();
    showToast(files.length > 1 ? `已保存 ${files.length} 个文件` : '已保存文件');
  }
}

async function createText(text, options = {}) {
  await api('/api/text', {
    method: 'POST',
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
    body: text,
  });

  if (!options.skipReload) {
    await loadItems();
  }
}

async function uploadFile(file) {
  const response = await api(`/api/upload?name=${encodeURIComponent(file.name || 'clipboard.bin')}`, {
    method: 'POST',
    headers: {
      'Content-Type': file.type || 'application/octet-stream',
    },
    body: file,
  });
  return response.json();
}

async function loadItems() {
  try {
    const response = await api('/api/items');
    state.items = await response.json();
    elements.serverState.textContent = '已连接';
    renderItems();
  } catch (error) {
    elements.serverState.textContent = error.message.includes('令牌') ? '需要令牌' : '连接失败';
    showToast(error.message);
  }
}

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (state.token) {
    headers.set('X-Clip-Token', state.token);
  }

  const response = await fetch(path, { ...options, headers });
  if (!response.ok) {
    throw new Error(await readError(response));
  }
  return response;
}

async function readError(response) {
  try {
    const payload = await response.json();
    return payload.error || response.statusText;
  } catch (_error) {
    return response.statusText || '请求失败';
  }
}

function renderItems() {
  const query = state.filter.trim().toLowerCase();
  const filtered = state.items.filter((item) => matchesQuery(item, query));
  elements.items.replaceChildren(...filtered.map(renderItem));
  elements.itemCount.textContent = `${filtered.length} 条`;

  if (!filtered.length) {
    const empty = document.createElement('p');
    empty.className = 'empty muted';
    empty.textContent = query ? '没有匹配条目' : '暂无条目';
    elements.items.append(empty);
  }
}

function renderItem(item) {
  const article = document.createElement('article');
  article.className = `item item-${item.kind}`;

  const header = document.createElement('header');
  header.className = 'item-head';

  const titleWrap = document.createElement('div');
  const title = document.createElement('h3');
  title.textContent = item.kind === 'text' ? textTitle(item.text) : item.fileName;
  const meta = document.createElement('p');
  meta.className = 'muted item-meta';
  meta.textContent = `${item.kind === 'text' ? '文本' : fileKind(item.mime)} · ${formatBytes(item.bytes)} · ${formatTime(item.createdAt)}`;
  titleWrap.append(title, meta);

  const actions = document.createElement('div');
  actions.className = 'item-actions';
  if (item.kind === 'text') {
    actions.append(button('复制', () => copyText(item.text || '')));
  } else {
    const openLink = document.createElement('a');
    openLink.className = 'button';
    openLink.href = withToken(item.url || '#');
    openLink.target = '_blank';
    openLink.rel = 'noreferrer';
    openLink.textContent = '打开';
    const downloadLink = document.createElement('a');
    downloadLink.className = 'button';
    downloadLink.href = withToken(`${item.url}?download=1`);
    downloadLink.textContent = '下载';
    actions.append(openLink, downloadLink);
  }
  actions.append(button('删除', () => removeItem(item.id), 'danger'));

  header.append(titleWrap, actions);
  article.append(header);

  if (item.kind === 'text') {
    const pre = document.createElement('pre');
    pre.className = 'text-preview';
    pre.textContent = item.text || '';
    article.append(pre);
  } else if ((item.mime || '').startsWith('image/')) {
    const image = document.createElement('img');
    image.className = 'image-preview';
    image.alt = item.fileName || 'image';
    image.loading = 'lazy';
    image.src = withToken(item.url || '#');
    article.append(image);
  }

  return article;
}

function matchesQuery(item, query) {
  if (!query) {
    return true;
  }
  const haystack = [item.kind, item.text, item.fileName, item.mime].filter(Boolean).join('\n').toLowerCase();
  return haystack.includes(query);
}

function textTitle(text = '') {
  const firstLine = text.split(/\r?\n/).find(Boolean) || '文本';
  return firstLine.length > 64 ? `${firstLine.slice(0, 64)}...` : firstLine;
}

function fileKind(mime = '') {
  if (mime.startsWith('image/')) {
    return '图片';
  }
  if (mime.startsWith('text/')) {
    return '文本文件';
  }
  return '文件';
}

function button(label, onClick, variant = '') {
  const element = document.createElement('button');
  element.type = 'button';
  element.textContent = label;
  if (variant) {
    element.classList.add(variant);
  }
  element.addEventListener('click', onClick);
  return element;
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    showToast('已复制');
  } catch (_error) {
    const textarea = document.createElement('textarea');
    textarea.value = text;
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.append(textarea);
    textarea.select();
    document.execCommand('copy');
    textarea.remove();
    showToast('已复制');
  }
}

async function removeItem(id) {
  if (!confirm('删除这个条目？')) {
    return;
  }

  try {
    await api(`/api/items/${encodeURIComponent(id)}`, { method: 'DELETE' });
    state.items = state.items.filter((item) => item.id !== id);
    renderItems();
    showToast('已删除');
  } catch (error) {
    showToast(error.message);
  }
}

function withToken(path) {
  const url = new URL(path, window.location.origin);
  if (state.token) {
    url.searchParams.set('token', state.token);
  }
  return `${url.pathname}${url.search}${url.hash}`;
}

function formatTime(value) {
  if (!value) {
    return '';
  }
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value));
}

function formatBytes(bytes = 0) {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.classList.add('is-visible');
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => elements.toast.classList.remove('is-visible'), 2400);
}
