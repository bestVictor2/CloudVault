const DEFAULT_BASE = "http://localhost:8000/api";
const FILE_PAGE_SIZE = 200;
const TOKEN_STORAGE_KEY = "go_pan_token";
const USER_PROFILE_STORAGE_KEY = "go_pan_user_profile";

const state = {
  apiBase: (localStorage.getItem("go_pan_api_base") || DEFAULT_BASE).replace("http://localhost:8080/api", DEFAULT_BASE),
  token: sessionStorage.getItem(TOKEN_STORAGE_KEY) || "",
  userProfile: (() => {
    try {
      const raw = localStorage.getItem(USER_PROFILE_STORAGE_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (err) {
      return null;
    }
  })(),
  currentFolderId: 0,
  folderStack: [{ id: 0, name: "root" }],
  files: [],
  searchMode: false,
  selected: new Map(),
  folderCache: {},
  recycleFiles: [],
  recycleSelected: new Map(),
};

function $(id) {
  return document.getElementById(id);
}

function decodeJWT(token) {
  if (!token || token.split(".").length < 2) return null;
  try {
    const payload = token.split(".")[1];
    const normalized = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padLength = (4 - (normalized.length % 4)) % 4;
    const padded = normalized + "=".repeat(padLength);
    const json = atob(padded);
    return JSON.parse(decodeURIComponent(escape(json)));
  } catch (err) {
    return null;
  }
}

function normalizeUserProfile(raw) {
  if (!raw || typeof raw !== "object") return null;
  const id = raw.id ?? raw.ID ?? raw.user_id ?? raw.userId ?? 0;
  if (!id) return null;
  return {
    id,
    username: raw.user_name ?? raw.UserName ?? raw.username ?? "",
    nickName: raw.nick_name ?? raw.NickName ?? raw.nickname ?? "",
    email: raw.email ?? raw.Email ?? "",
    avatarURL: raw.avatar_url ?? raw.AvatarURL ?? raw.avatar ?? "",
    bio: raw.bio ?? raw.Bio ?? "",
    isActive: Boolean(raw.is_active ?? raw.IsActive ?? false),
    totalSpace: Number(raw.total_space ?? raw.TotalSpace ?? 0),
    useSpace: Number(raw.use_space ?? raw.UseSpace ?? 0),
    createdAt: raw.created_at ?? raw.CreatedAt ?? "",
  };
}

function setUserProfile(raw) {
  const profile = normalizeUserProfile(raw);
  state.userProfile = profile;
  if (profile) {
    localStorage.setItem(USER_PROFILE_STORAGE_KEY, JSON.stringify(profile));
  } else {
    localStorage.removeItem(USER_PROFILE_STORAGE_KEY);
  }
  updateAuthUI();
}

function getApiBase() {
  const input = $("apiBase");
  const base = input ? input.value.trim() : state.apiBase;
  return base.replace(/\/$/, "");
}

function saveBase() {
  state.apiBase = getApiBase();
  localStorage.setItem("go_pan_api_base", state.apiBase);
}

function setToken(token) {
  state.token = token || "";
  if (state.token) {
    sessionStorage.setItem(TOKEN_STORAGE_KEY, state.token);
    // Clear legacy persisted token to enforce fresh login after browser restart.
    localStorage.removeItem(TOKEN_STORAGE_KEY);
  } else {
    sessionStorage.removeItem(TOKEN_STORAGE_KEY);
    localStorage.removeItem(TOKEN_STORAGE_KEY);
    setUserProfile(null);
  }
  updateAuthUI();
}

function updateAuthUI() {
  const payload = decodeJWT(state.token);
  const authState = $("authState");
  const authToken = $("authToken");
  const authUser = $("authUser");

  if (payload) {
    if (authState) authState.textContent = "Logged in";
    if (authToken) authToken.textContent = `${state.token.slice(0, 10)}...${state.token.slice(-6)}`;
    const userLabel = state.userProfile?.username || payload.username || "用户";
    const userId = state.userProfile?.id || payload.user_id || "-";
    if (authUser) authUser.textContent = `${userLabel} (#${userId})`;
  } else {
    if (authState) authState.textContent = "Not logged in";
    if (authToken) authToken.textContent = "-";
    if (authUser) authUser.textContent = "-";
  }
  renderProfileIdentity();
}

function setStatus(el, message, isError = false) {
  if (!el) return;
  el.textContent = message;
  el.classList.toggle("error", Boolean(isError));
}

function formatSize(bytes) {
  if (bytes === null || bytes === undefined) return "-";
  const size = Number(bytes);
  if (Number.isNaN(size)) return "-";
  if (size === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = size;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  const precision = value >= 10 || index === 0 ? 0 : 1;
  return `${value.toFixed(precision)} ${units[index]}`;
}

function saveLastSelection(file) {
  if (!file) return;
  const payload = {
    id: file.id,
    name: file.name,
    is_dir: Boolean(file.is_dir),
    parent_id: file.parent_id || 0,
  };
  localStorage.setItem("go_pan_last_selection", JSON.stringify(payload));
}

function loadLastSelection() {
  try {
    const raw = localStorage.getItem("go_pan_last_selection");
    if (!raw) return null;
    return JSON.parse(raw);
  } catch (err) {
    return null;
  }
}

function saveLastFolder() {
  const stack = state.folderStack || [];
  const path =
      stack.length <= 1 ? "/" : `/${stack.slice(1).map((item) => item.name).join("/")}`;
  localStorage.setItem(
      "go_pan_last_folder",
      JSON.stringify({ id: state.currentFolderId || 0, path })
  );
}

function loadLastFolder() {
  try {
    const raw = localStorage.getItem("go_pan_last_folder");
    if (!raw) return null;
    return JSON.parse(raw);
  } catch (err) {
    return null;
  }
}

function describeSelection(file) {
  if (!file) return "No selection.";
  return file.is_dir ? `文件夹：${file.name}` : `文件：${file.name}`;
}

function updateSelectionSummary() {
  const el = $("selectionSummary");
  if (!el) return;
  const items = Array.from(state.selected.values());
  if (items.length === 0) {
    el.textContent = "No selection.";
    return;
  }
  const names = items.slice(0, 3).map((item) => item.name).join(", ");
  const extra = items.length > 3 ? ` +${items.length - 3}` : "";
  el.textContent = `Selected ${items.length}: ${names}${extra}`;
}

function updateRecycleSelectionSummary() {
  const el = $("recycleSelectionSummary");
  if (!el) return;
  const items = Array.from(state.recycleSelected.values());
  if (items.length === 0) {
    el.textContent = "No selection.";
    return;
  }
  const names = items.slice(0, 3).map((item) => item.name).join(", ");
  const extra = items.length > 3 ? ` +${items.length - 3}` : "";
  el.textContent = `Selected ${items.length}: ${names}${extra}`;
}

function toggleRecycleSelectAll(checked) {
  state.recycleSelected.clear();
  const rows = document.querySelectorAll("#recycleRows .row");
  rows.forEach((row) => {
    const id = Number(row.dataset.id);
    const file = state.recycleFiles.find((item) => item.id === id);
    if (!file) return;
    const box = row.querySelector("input[type=checkbox]");
    if (box) box.checked = checked;
    row.classList.toggle("selected", checked);
    if (checked) {
      state.recycleSelected.set(file.id, file);
    }
  });
  updateRecycleSelectionSummary();
}

function renderStoredSelection(el) {
  if (!el) return;
  const selection = loadLastSelection();
  el.textContent = selection ? describeSelection(selection) : "No selection.";
}

function parseIds(value) {
  return value
      .split(/[\s,]+/)
      .map((item) => Number(item.trim()))
      .filter((num) => !Number.isNaN(num) && num > 0);
}

function unwrap(data) {
  if (data && typeof data === "object" && "code" in data && "data" in data) {
    return data.data;
  }
  return data;
}

async function apiFetch(path, options = {}) {
  const headers = new Headers(options.headers || {});
  const isForm = options.body instanceof FormData;
  if (!isForm && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  headers.set("Accept", "application/json");
  if (state.token) {
    headers.set("Authorization", `Bearer ${state.token}`);
  }

  const response = await fetch(`${getApiBase()}${path}`, {
    method: options.method || "GET",
    headers,
    body: options.body,
  });

  const contentType = response.headers.get("content-type") || "";
  let data = null;
  if (contentType.includes("application/json")) {
    data = await response.json();
  } else {
    data = await response.text();
  }

  if (!response.ok) {
    const message = data?.error || data?.msg || data?.message || "请求失败";
    const err = new Error(message);
    err.payload = data;
    throw err;
  }

  return data;
}

function getFilenameFromDisposition(disposition) {
  if (!disposition) return "";
  const utf8Match = disposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utf8Match) {
    try {
      return decodeURIComponent(utf8Match[1]);
    } catch (err) {
      return utf8Match[1];
    }
  }
  const quoted = disposition.match(/filename="([^"]+)"/i);
  if (quoted) return quoted[1];
  const plain = disposition.match(/filename=([^;]+)/i);
  if (plain) return plain[1].trim();
  return "";
}

async function apiFetchBlob(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (state.token) {
    headers.set("Authorization", `Bearer ${state.token}`);
  }

  const response = await fetch(`${getApiBase()}${path}`, {
    method: options.method || "POST",
    headers,
    body: options.body,
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || "下载失败");
  }

  const blob = await response.blob();
  const filename = getFilenameFromDisposition(response.headers.get("content-disposition"));
  return { blob, filename };
}

function initBaseControls() {
  const input = $("apiBase");
  if (input) {
    input.value = state.apiBase;
    input.addEventListener("change", saveBase);
  }
  const button = $("saveBase");
  if (button) {
    button.addEventListener("click", saveBase);
  }
  const logoutBtn = $("logoutBtn");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", () => setToken(""));
  }
  updateAuthUI();
}

async function handleLogin() {
  const status = $("loginStatus");
  try {
    if (decodeJWT(state.token)) {
      setStatus(status, "当前已登录，请先退出后再登录。", true);
      return;
    }
    setStatus(status, "正在登录...");
    const data = await apiFetch("/login", {
      method: "POST",
      body: JSON.stringify({
        username: $("loginUsername").value.trim(),
        password: $("loginPassword").value,
      }),
    });
    if (!data?.token) {
      throw new Error("登录响应缺少 token");
    }
    setToken(data.token);
    if (data?.user) {
      setUserProfile(data.user);
    }
    setStatus(status, "Login successful.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleRegister() {
  const status = $("registerStatus");
  try {
    setStatus(status, "正在提交注册...");
    await apiFetch("/register", {
      method: "POST",
      body: JSON.stringify({
        username: $("registerUsername").value.trim(),
        "first-password": $("registerPassword").value,
        "second-password": $("registerPassword2").value,
        email: $("registerEmail").value.trim(),
      }),
    });
    setStatus(status, "Activation email sent.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleActivate() {
  const status = $("activateStatus");
  try {
    setStatus(status, "正在激活...");
    const token = $("activateToken").value.trim();
    const data = await apiFetch(`/activate?token=${encodeURIComponent(token)}`, {
      method: "GET",
    });
    setStatus(status, data.msg || "Activated.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initAuthPage() {
  const loginBtn = $("loginBtn");
  const registerBtn = $("registerBtn");
  const activateBtn = $("activateBtn");
  if (loginBtn) loginBtn.addEventListener("click", handleLogin);
  if (registerBtn) registerBtn.addEventListener("click", handleRegister);
  if (activateBtn) activateBtn.addEventListener("click", handleActivate);
}

function getSelectedFiles() {
  return Array.from(state.selected.values());
}

function setFileList(files) {
  state.files = files;
  state.selected.clear();
  updateSelectionSummary();
  const selectAll = $("selectAll");
  if (selectAll) selectAll.checked = false;
  renderFiles($("listRows"), files);
}

function clearSelection() {
  state.selected.clear();
  const rows = document.querySelectorAll("#listRows .row");
  rows.forEach((row) => {
    const box = row.querySelector("input[type=checkbox]");
    if (box) box.checked = false;
    row.classList.remove("selected");
  });
  const selectAll = $("selectAll");
  if (selectAll) selectAll.checked = false;
  updateSelectionSummary();
  const status = $("listStatus");
  if (status) setStatus(status, "Selection cleared.");
}

function toggleSelectAll(checked) {
  state.selected.clear();
  const rows = document.querySelectorAll("#listRows .row");
  rows.forEach((row) => {
    const id = Number(row.dataset.id);
    const file = state.files.find((item) => item.id === id);
    if (!file) return;
    const box = row.querySelector("input[type=checkbox]");
    if (box) box.checked = checked;
    row.classList.toggle("selected", checked);
    if (checked) {
      state.selected.set(file.id, file);
      saveLastSelection(file);
    }
  });
  updateSelectionSummary();
}

function updateBreadcrumb() {
  const container = $("breadcrumb");
  if (!container) return;
  container.innerHTML = "";
  state.folderStack.forEach((item, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = item.name || "Root";
    if (index === state.folderStack.length - 1) {
      button.disabled = true;
    } else {
      button.addEventListener("click", () => {
        state.folderStack = state.folderStack.slice(0, index + 1);
        state.currentFolderId = item.id;
        refreshCurrentFolder();
      });
    }
    container.appendChild(button);
  });
}

async function refreshCurrentFolder() {
  const status = $("listStatus");
  try {
    state.searchMode = false;
    setStatus(status, "正在加载文件...");
    const data = await apiFetch("/file/list", {
      method: "POST",
      body: JSON.stringify({
        parent_id: state.currentFolderId,
        page: 1,
        page_size: FILE_PAGE_SIZE,
        order_by: "created_at",
        order_desc: false,
      }),
    });
    const result = unwrap(data);
    const files = result.files || [];
    setFileList(files);
    updateBreadcrumb();
    saveLastFolder();
    setStatus(status, `Loaded ${files.length} items.`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function openFolder(file) {
  if (!file || !file.is_dir) return;
  state.folderStack.push({ id: file.id, name: file.name });
  state.currentFolderId = file.id;
  refreshCurrentFolder();
}

function goUpFolder() {
  if (state.folderStack.length <= 1) return;
  state.folderStack.pop();
  const current = state.folderStack[state.folderStack.length - 1];
  state.currentFolderId = current.id;
  refreshCurrentFolder();
}

async function restoreLastFolder() {
  state.folderCache = {};
  const stored = loadLastFolder();
  if (!stored || !stored.path || stored.path === "/") {
    state.folderStack = [{ id: 0, name: "Root" }];
    state.currentFolderId = 0;
    await refreshCurrentFolder();
    return;
  }
  const parts = stored.path.replace(/\\/g, "/").split("/").filter(Boolean);
  let current = 0;
  const stack = [{ id: 0, name: "Root" }];
  try {
    for (const part of parts) {
      const id = await resolveFolderId(current, part, state.folderCache);
      if (!id) {
        throw new Error("Folder not found");
      }
      stack.push({ id, name: part });
      current = id;
    }
    state.folderStack = stack;
    state.currentFolderId = current;
  } catch (err) {
    state.folderStack = [{ id: 0, name: "Root" }];
    state.currentFolderId = 0;
  }
  await refreshCurrentFolder();
}

async function handleFileSearch() {
  const status = $("listStatus");
  const query = $("searchQuery")?.value.trim() || "";
  if (!query) {
    state.searchMode = false;
    refreshCurrentFolder();
    return;
  }
  try {
    setStatus(status, "正在搜索...");
    const payload = {
      query,
      page: 1,
      page_size: FILE_PAGE_SIZE,
      order_by: "created_at",
      order_desc: false,
    };
    if ($("searchInCurrent")?.checked) {
      payload.parent_id = state.currentFolderId;
    }
    const data = await apiFetch("/file/search", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    const result = unwrap(data);
    const files = result.files || [];
    state.searchMode = true;
    setFileList(files);
    setStatus(status, `Found ${files.length} items.`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function renderFiles(container, files) {
  if (!container) return;
  container.innerHTML = "";
  files.forEach((file) => {
    const row = document.createElement("div");
    row.className = "row";
    row.dataset.id = String(file.id);

    const checkCell = document.createElement("span");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.checked = state.selected.has(file.id);
    checkbox.addEventListener("change", () => {
      if (checkbox.checked) {
        state.selected.set(file.id, file);
        saveLastSelection(file);
      } else {
        state.selected.delete(file.id);
      }
      row.classList.toggle("selected", checkbox.checked);
      const selectAll = $("selectAll");
      if (selectAll) {
        selectAll.checked = state.selected.size === state.files.length && state.files.length > 0;
      }
      updateSelectionSummary();
    });
    checkCell.appendChild(checkbox);

    const nameCell = document.createElement("span");
    nameCell.className = `file-name${file.is_dir ? " is-dir" : ""}`;
    nameCell.textContent = file.name;
    if (file.is_dir) {
      nameCell.addEventListener("click", () => openFolder(file));
    }

    const typeCell = document.createElement("span");
    typeCell.textContent = file.is_dir ? "Folder" : "File";

    const sizeCell = document.createElement("span");
    sizeCell.textContent = file.is_dir ? "-" : formatSize(file.size);

    const updatedCell = document.createElement("span");
    updatedCell.textContent = formatDate(file.updated_at || file.updatedAt || file.UpdatedAt);

    row.appendChild(checkCell);
    row.appendChild(nameCell);
    row.appendChild(typeCell);
    row.appendChild(sizeCell);
    row.appendChild(updatedCell);

    if (state.selected.has(file.id)) {
      row.classList.add("selected");
    }
    container.appendChild(row);
  });
}

function resolveSelectionList() {
  const items = getSelectedFiles();
  if (items.length === 0) {
    return { error: "Please select at least one item." };
  }
  return { items };
}

function resolveSingleSelection({ allowStored = false } = {}) {
  const items = getSelectedFiles();
  if (items.length > 1) {
    return { error: "Please select exactly one item." };
  }
  let item = items[0];
  if (!item && allowStored) {
    item = loadLastSelection();
  }
  if (!item) {
    return { error: "Please select one item first." };
  }
  return { item };
}

function resolveSingleFile({ allowStored = false } = {}) {
  const result = resolveSingleSelection({ allowStored });
  if (result.error) {
    return result;
  }
  if (result.item.is_dir) {
    return { error: "A file is required, folder is not allowed." };
  }
  return { file: result.item };
}

async function handleRename() {
  const status = $("renameStatus");
  try {
    const selection = resolveSingleSelection();
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    const newName = $("renameNewName").value.trim();
    if (!newName) {
      setStatus(status, "Please enter a new name.", true);
      return;
    }
    setStatus(status, "正在重命名...");
    await apiFetch("/file/rename", {
      method: "POST",
      body: JSON.stringify({
        file_id: selection.item.id,
        new_name: newName,
      }),
    });
    setStatus(status, "Rename completed.");
    refreshCurrentFolder();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleMoveCopy(isCopy) {
  const status = $("moveStatus");
  try {
    const selection = resolveSelectionList();
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    setStatus(status, isCopy ? "正在复制..." : "正在移动...");
    const targetPath = $("moveTargetPath")?.value || "";
    const targetId = await resolvePathToFolderId(targetPath, {
      baseId: state.currentFolderId,
      createMissing: false,
    });
    const payload = { file_ids: selection.items.map((item) => item.id), target_id: targetId };
    await apiFetch(isCopy ? "/file/copy" : "/file/move", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    setStatus(status, isCopy ? "Copy completed." : "Move completed.");
    refreshCurrentFolder();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleDeleteToRecycle() {
  const status = $("deleteStatus");
  try {
    const selection = resolveSelectionList();
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    setStatus(status, "正在移入回收站...");
    await apiFetch("/file/delete", {
      method: "POST",
      body: JSON.stringify({ file_ids: selection.items.map((item) => item.id) }),
    });
    setStatus(status, "Moved to recycle bin.");
    refreshCurrentFolder();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleCreateFolder() {
  const status = $("folderStatus");
  try {
    const name = $("folderName").value.trim();
    if (!name) {
      setStatus(status, "Please enter folder name.", true);
      return;
    }
    setStatus(status, "正在创建文件夹...");
    await apiFetch("/file/folder", {
      method: "POST",
      body: JSON.stringify({
        parent_id: state.currentFolderId,
        name,
      }),
    });
    setStatus(status, "Folder created.");
    refreshCurrentFolder();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handlePreviewLink() {
  const status = $("previewStatus");
  try {
    const selection = resolveSingleFile({ allowStored: true });
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    setStatus(status, "正在获取预览链接...");
    const data = await apiFetch(`/file/preview/${selection.file.id}`, { method: "GET" });
    status.innerHTML = `预览链接：<a href="${data.url}" target="_blank">${data.url}</a>`;
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleMinioDownload() {
  const status = $("minioStatus");
  try {
    setStatus(status, "Requesting download URL...");
    const selection = resolveSingleFile({ allowStored: true });
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    const payload = { file_id: selection.file.id };
    const data = await apiFetch("/file/download/url", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    const downloadURL = data?.url || data?.download_url || data?.downloadURL;
    if (!downloadURL) {
      throw new Error("Download URL missing.");
    }
    if (status) {
      status.innerHTML = `Direct link: <a href="${downloadURL}" target="_blank" rel="noopener">${downloadURL}</a>`;
    }
    window.open(downloadURL, "_blank", "noopener");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleArchiveDownload() {
  const status = $("archiveStatus");
  try {
    const selection = resolveSelectionList();
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    setStatus(status, "正在准备压缩包...");
    const name = $("archiveName").value.trim() || "archive.zip";
    const result = await apiFetchBlob("/file/download/archive", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ file_ids: selection.items.map((item) => item.id), name }),
    });
    const url = URL.createObjectURL(result.blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = result.filename || name;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
    setStatus(status, "Archive download started.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initFilesPage() {
  const searchBtn = $("searchBtn");
  const clearSearchBtn = $("clearSearchBtn");
  const refreshBtn = $("refreshBtn");
  const upBtn = $("upBtn");
  const renameBtn = $("renameBtn");
  const moveBtn = $("moveBtn");
  const copyBtn = $("copyBtn");
  const deleteBtn = $("deleteBtn");
  const folderBtn = $("folderBtn");
  const previewBtn = $("previewBtn");
  const minioBtn = $("minioBtn");
  const archiveBtn = $("archiveBtn");
  const selectAll = $("selectAll");
  const clearSelectionBtn = $("clearSelectionBtn");

  if (searchBtn) searchBtn.addEventListener("click", handleFileSearch);
  if (clearSearchBtn) {
    clearSearchBtn.addEventListener("click", () => {
      const query = $("searchQuery");
      if (query) query.value = "";
      state.searchMode = false;
      const status = $("listStatus");
      if (status) setStatus(status, "Search conditions cleared.");
      refreshCurrentFolder();
    });
  }
  if (refreshBtn) refreshBtn.addEventListener("click", refreshCurrentFolder);
  if (upBtn) upBtn.addEventListener("click", goUpFolder);
  if (renameBtn) renameBtn.addEventListener("click", handleRename);
  if (moveBtn) moveBtn.addEventListener("click", () => handleMoveCopy(false));
  if (copyBtn) copyBtn.addEventListener("click", () => handleMoveCopy(true));
  if (deleteBtn) deleteBtn.addEventListener("click", handleDeleteToRecycle);
  if (folderBtn) folderBtn.addEventListener("click", handleCreateFolder);
  if (previewBtn) previewBtn.addEventListener("click", handlePreviewLink);
  if (minioBtn) minioBtn.addEventListener("click", handleMinioDownload);
  if (archiveBtn) archiveBtn.addEventListener("click", handleArchiveDownload);
  if (selectAll) {
    selectAll.addEventListener("change", (event) => {
      toggleSelectAll(event.target.checked);
    });
  }
  if (clearSelectionBtn) clearSelectionBtn.addEventListener("click", clearSelection);

  restoreLastFolder();
}

async function hashFile(file) {
  const buffer = await file.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-256", buffer);
  return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
}

function describeUploadTarget(path) {
  const normalized = normalizePath(path);
  return normalized ? normalized : "/";
}

async function resolveUploadTarget() {
  const status = $("uploadTargetStatus");
  const input = $("uploadTargetPath");
  const createMissing = $("uploadCreateMissing")?.checked;
  const stored = loadLastFolder();
  const baseId = stored?.id || 0;
  const rawPath = input ? input.value : "";
  try {
    const parentId = await resolvePathToFolderId(rawPath, {
      baseId,
      createMissing: Boolean(createMissing),
    });
    if (status) {
      const label = rawPath ? describeUploadTarget(rawPath) : stored?.path || "/";
      setStatus(status, `目标目录已就绪：${label}`);
    }
    return parentId;
  } catch (err) {
    if (status) {
      setStatus(status, err.message, true);
    }
    throw err;
  }
}

function applyLastUploadTarget() {
  const status = $("uploadTargetStatus");
  const input = $("uploadTargetPath");
  const stored = loadLastFolder();
  if (!input) return;
  if (stored?.path) {
    input.value = stored.path;
    setStatus(status, `已切换至：${stored.path}`);
  } else {
    setStatus(status, "No target folder selected yet.", true);
  }
}

async function handleInstantHash() {
  const status = $("instantStatus");
  try {
    const file = $("instantFile").files[0];
    if (!file) {
      setStatus(status, "Please select a file first.", true);
      return;
    }
    setStatus(status, "正在计算哈希...");
    const hash = await hashFile(file);
    $("instantName").value = file.name;
    $("instantSize").value = file.size;
    $("instantHash").value = hash;
    setStatus(status, `哈希完成：${hash.slice(0, 12)}...`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleInstantUpload() {
  const status = $("instantStatus");
  try {
    const file = $("instantFile").files[0];
    if (!file) {
      setStatus(status, "Please select source file first.", true);
      return;
    }

    const nameInput = $("instantName");
    const sizeInput = $("instantSize");
    const hashInput = $("instantHash");
    if (nameInput) nameInput.value = file.name;
    if (sizeInput) sizeInput.value = file.size;

    let hash = hashInput ? hashInput.value.trim() : "";
    if (!hash) {
      setStatus(status, "正在计算哈希...");
      hash = await hashFile(file);
      if (hashInput) hashInput.value = hash;
    }

    setStatus(status, "正在提交秒传...");
    const parentId = await resolveUploadTarget();
    const data = await apiFetch("/file/upload/hash", {
      method: "POST",
      body: JSON.stringify({
        file_id: 0,
        file_name: file.name,
        size: Number(file.size),
        hash,
        parent_id: parentId,
        is_dir: false,
      }),
    });
    const result = unwrap(data) || {};
    const instantHit = result.instant === true && Number(result.file_id) > 0;
    if (instantHit) {
      setStatus(status, "秒传成功。");
    } else {
      const reason = String(result.reason || "");
      if (reason === "object_missing") {
        setStatus(status, "秒传记录存在但对象缺失，正在自动切换普通上传...");
      } else if (reason === "size_mismatch") {
        setStatus(status, "哈希命中但大小不一致，正在自动切换普通上传...");
      } else if (result.instant === true && !result.file_id) {
        setStatus(status, "秒传返回异常，正在自动切换普通上传...");
      } else {
        setStatus(status, "未命中秒传，正在自动切换普通上传...");
      }
      const fallback = await uploadByMultipart(file, parentId, status, null, hash);
      if (fallback.instant) {
        setStatus(status, "未命中秒传后重试命中秒传。");
      } else {
        setStatus(status, "未命中秒传，首次已使用普通上传完成。");
      }
    }
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function uploadByMultipart(file, parentId, status, bar, knownHash = "") {
  const payload = decodeJWT(state.token);
  if (!payload) {
    throw new Error("Login required.");
  }

  let hash = knownHash;
  if (!hash) {
    setStatus(status, "正在计算哈希...");
    hash = await hashFile(file);
  }
  const chunkSizeMB = Number($("multipartChunkSize")?.value) || 5;
  const chunkSize = Math.max(1, chunkSizeMB) * 1024 * 1024;
  const totalChunks = Math.max(1, Math.ceil(file.size / chunkSize));

  const initData = await apiFetch("/file/upload/multipart/init", {
    method: "POST",
    body: JSON.stringify({
      user_id: payload.user_id,
      file_id: 0,
      file_name: file.name,
      size: file.size,
      hash,
      chunk_size: chunkSize,
      total_chunks: totalChunks,
      parent_id: parentId,
    }),
  });

  const result = unwrap(initData) || {};
  if (result.instant) {
    if (bar) bar.style.width = "100%";
    return { instant: true };
  }

  const uploadId = result.upload_id;
  if (!uploadId) {
    throw new Error("Missing upload ID.");
  }

  const uploaded = new Set(result.uploaded || []);
  let completed = 0;
  for (let index = 0; index < totalChunks; index += 1) {
    if (uploaded.has(index)) {
      completed += 1;
      if (bar) {
        bar.style.width = `${Math.round((completed / totalChunks) * 100)}%`;
      }
      continue;
    }
    const start = index * chunkSize;
    const end = Math.min(file.size, start + chunkSize);
    const chunk = file.slice(start, end);
    const form = new FormData();
    form.append("chunk_index", String(index));
    form.append("upload_id", uploadId);
    form.append("chunk", chunk, file.name);
    setStatus(status, `正在上传分片 ${index + 1}/${totalChunks}`);
    await apiFetch("/file/upload/multipart/chunk", {
      method: "POST",
      body: form,
    });
    completed += 1;
    if (bar) {
      bar.style.width = `${Math.round((completed / totalChunks) * 100)}%`;
    }
  }

  setStatus(status, "正在完成上传...");
  await apiFetch("/file/upload/multipart/complete", {
    method: "POST",
    body: JSON.stringify({
      file_id: 0,
      file_hash: hash,
      file_name: file.name,
      file_size: file.size,
      total_chunks: totalChunks,
      parent_id: parentId,
      is_dir: false,
    }),
  });
  if (bar) bar.style.width = "100%";
  return { instant: false };
}

async function handleMultipartUpload() {
  const status = $("multipartStatus");
  const bar = $("multipartBar");
  try {
    const file = $("multipartFile").files[0];
    if (!file) {
      setStatus(status, "Please select a file first.", true);
      return;
    }
    const parentId = await resolveUploadTarget();
    const result = await uploadByMultipart(file, parentId, status, bar);
    if (result.instant) {
      setStatus(status, "Instant upload hit, multipart skipped.");
      return;
    }
    setStatus(status, "Multipart upload completed.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function listChildren(parentId) {
  const data = await apiFetch("/file/list", {
    method: "POST",
    body: JSON.stringify({
      parent_id: parentId,
      page: 1,
      page_size: 2000,
      order_by: "created_at",
      order_desc: false,
    }),
  });
  const result = unwrap(data);
  return result.files || [];
}

async function resolveFolderId(parentId, name, cache) {
  if (!cache[parentId]) {
    const children = await listChildren(parentId);
    cache[parentId] = new Map();
    children.forEach((item) => {
      if (item.is_dir) {
        cache[parentId].set(item.name, item.id);
      }
    });
  }
  return cache[parentId].get(name) || 0;
}

async function ensureFolder(parentId, name, cache) {
  let folderId = await resolveFolderId(parentId, name, cache);
  if (folderId) return folderId;

  try {
    await apiFetch("/file/folder", {
      method: "POST",
      body: JSON.stringify({ parent_id: parentId, name }),
    });
  } catch (err) {
    const message = (err.message || "").toLowerCase();
    if (!message.includes("exists")) {
      throw err;
    }
  }

  delete cache[parentId];
  folderId = await resolveFolderId(parentId, name, cache);
  if (!folderId) {
    throw new Error(`创建后未找到文件夹：${name}`);
  }
  return folderId;
}

async function ensureFolderPath(baseParentId, pathParts, cache, map) {
  if (!pathParts.length) return baseParentId;
  let current = baseParentId;
  let currentPath = "";
  for (const part of pathParts) {
    currentPath = currentPath ? `${currentPath}/${part}` : part;
    if (map[currentPath]) {
      current = map[currentPath];
      continue;
    }
    current = await ensureFolder(current, part, cache);
    map[currentPath] = current;
  }
  return current;
}

function normalizePath(value) {
  return (value || "").trim().replace(/\\/g, "/");
}

async function resolvePathToFolderId(path, { baseId = 0, createMissing = false } = {}) {
  const normalized = normalizePath(path);
  if (!normalized || normalized === ".") {
    return baseId;
  }
  const lower = normalized.toLowerCase();
  if (normalized === "/" || lower === "root") {
    return 0;
  }

  let effectiveBase = baseId;
  let working = normalized;
  if (normalized.startsWith("/")) {
    effectiveBase = 0;
    working = normalized.slice(1);
  }
  const parts = working.split("/").filter(Boolean);
  if (!parts.length) {
    return effectiveBase;
  }

  if (createMissing) {
    return ensureFolderPath(effectiveBase, parts, state.folderCache, {});
  }

  let current = effectiveBase;
  for (const part of parts) {
    const next = await resolveFolderId(current, part, state.folderCache);
    if (!next) {
      throw new Error(`未找到目录：${part}`);
    }
    current = next;
  }
  return current;
}

async function uploadFileMultipartForFolder(file, parentId, chunkSizeMB, statusCb) {
  const payload = decodeJWT(state.token);
  if (!payload) {
    throw new Error("Login required.");
  }

  const hash = await hashFile(file);
  const chunkSize = Math.max(1, chunkSizeMB) * 1024 * 1024;
  const totalChunks = Math.max(1, Math.ceil(file.size / chunkSize));

  const initData = await apiFetch("/file/upload/multipart/init", {
    method: "POST",
    body: JSON.stringify({
      user_id: payload.user_id,
      file_id: 0,
      file_name: file.name,
      size: file.size,
      hash,
      chunk_size: chunkSize,
      total_chunks: totalChunks,
      parent_id: parentId,
    }),
  });

  const result = unwrap(initData) || {};
  if (result.instant) {
    if (statusCb) {
      statusCb(`已秒传 ${file.name}`);
    }
    return;
  }

  const uploadId = result.upload_id;
  if (!uploadId) {
    throw new Error("Missing upload ID.");
  }

  const uploaded = new Set(result.uploaded || []);
  for (let index = 0; index < totalChunks; index += 1) {
    if (uploaded.has(index)) continue;
    const start = index * chunkSize;
    const end = Math.min(file.size, start + chunkSize);
    const chunk = file.slice(start, end);
    const form = new FormData();
    form.append("chunk_index", String(index));
    form.append("upload_id", uploadId);
    form.append("chunk", chunk, file.name);
    if (statusCb) {
      statusCb(`正在上传 ${file.name} (${index + 1}/${totalChunks})`);
    }
    await apiFetch("/file/upload/multipart/chunk", {
      method: "POST",
      body: form,
    });
  }

  await apiFetch("/file/upload/multipart/complete", {
    method: "POST",
    body: JSON.stringify({
      file_id: 0,
      file_hash: hash,
      file_name: file.name,
      file_size: file.size,
      total_chunks: totalChunks,
      parent_id: parentId,
      is_dir: false,
    }),
  });
}

async function handleFolderUpload() {
  const status = $("folderStatus");
  try {
    const files = Array.from($("folderInput").files || []);
    if (files.length === 0) {
      setStatus(status, "Please select a folder first.", true);
      return;
    }
    const baseParentId = await resolveUploadTarget();
    const chunkSizeMB = Number($("folderChunkSize").value) || 5;

    const folderCache = {};
    const pathMap = {};

    let completed = 0;
    for (const file of files) {
      const relativePath = file.webkitRelativePath || file.name;
      const parts = relativePath.split("/").filter(Boolean);
      const fileName = parts.pop();
      const parentId = await ensureFolderPath(baseParentId, parts, folderCache, pathMap);
      setStatus(status, `正在上传 ${fileName} (${completed + 1}/${files.length})`);
      await uploadFileMultipartForFolder(file, parentId, chunkSizeMB, (msg) => {
        setStatus(status, `${msg} (${completed + 1}/${files.length})`);
      });
      completed += 1;
      setStatus(status, `已上传 ${completed}/${files.length}`);
    }
    setStatus(status, "Folder upload completed.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleURLUpload() {
  const status = $("urlUploadStatus");
  try {
    const urlInput = $("urlUploadURL");
    const nameInput = $("urlUploadName");
    const rawURL = urlInput ? urlInput.value.trim() : "";
    if (!rawURL) {
      setStatus(status, "URL is required.", true);
      return;
    }
    const parentId = await resolveUploadTarget();
    let fileName = nameInput ? nameInput.value.trim() : "";
    if (!fileName) {
      fileName = guessFilenameFromURL(rawURL);
      if (nameInput && fileName) {
        nameInput.value = fileName;
      }
    }
    setStatus(status, "正在从 URL 导入...");
    const payload = { url: rawURL, parent_id: parentId };
    if (fileName) {
      payload.file_name = fileName;
    }
    const data = await apiFetch("/file/upload/url", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    const fileID = data?.file_id ?? data?.id ?? "-";
    const savedName = data?.name || fileName || "(unknown)";
    const sizeText = data?.size !== undefined ? formatSize(data.size) : "-";
    setStatus(status, `导入成功：${savedName} (#${fileID}, ${sizeText})`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initUploadPage() {
  const instantHashBtn = $("instantHashBtn");
  const instantUploadBtn = $("instantUploadBtn");
  const multipartBtn = $("multipartBtn");
  const urlUploadBtn = $("urlUploadBtn");
  const folderUploadBtn = $("folderUploadBtn");
  const uploadUseLastBtn = $("uploadUseLastBtn");
  const uploadTargetInput = $("uploadTargetPath");
  const uploadStatus = $("uploadTargetStatus");
  const instantFile = $("instantFile");

  if (instantHashBtn) instantHashBtn.addEventListener("click", handleInstantHash);
  if (instantUploadBtn) instantUploadBtn.addEventListener("click", handleInstantUpload);
  if (multipartBtn) multipartBtn.addEventListener("click", handleMultipartUpload);
  if (urlUploadBtn) urlUploadBtn.addEventListener("click", handleURLUpload);
  if (folderUploadBtn) folderUploadBtn.addEventListener("click", handleFolderUpload);
  if (uploadUseLastBtn) uploadUseLastBtn.addEventListener("click", applyLastUploadTarget);
  if (instantFile) instantFile.addEventListener("change", handleInstantHash);

  const stored = loadLastFolder();
  if (uploadTargetInput && stored?.path) {
    uploadTargetInput.value = stored.path;
    if (uploadStatus) {
      setStatus(uploadStatus, `已切换至：${stored.path}`);
    }
  }
}

async function handleShareCreate() {
  const status = $("shareStatus");
  try {
    const selection = resolveSingleSelection({ allowStored: true });
    if (selection.error) {
      setStatus(status, selection.error, true);
      return;
    }
    setStatus(status, "正在创建分享...");
    const data = await apiFetch("/share/create", {
      method: "POST",
      body: JSON.stringify({
        file_id: selection.item.id,
        expire_days: Number($("shareExpire").value),
        need_code: $("shareNeedCode").checked,
      }),
    });
    const base = getApiBase().replace(/\/api$/, "");
    const link = `${base}/api/share/download/${data.share_id}?extract_code=${encodeURIComponent(
        data.extract_code || ""
    )}`;
    status.innerHTML = `Share ID: ${data.share_id}<br>Code: ${
        data.extract_code || "-"
    }<br><a href="${link}" target="_blank">Open share</a>`;
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function handleShareDownload() {
  const status = $("shareDownloadStatus");
  const shareId = $("shareDownloadId").value.trim();
  const code = $("shareDownloadCode").value.trim();
  if (!shareId) {
    setStatus(status, "Share ID is required.", true);
    return;
  }
  const base = getApiBase().replace(/\/api$/, "");
  const link = `${base}/api/share/download/${shareId}?extract_code=${encodeURIComponent(code)}`;
  status.innerHTML = `下载链接：<a href="${link}" target="_blank">${link}</a>`;
}

function initSharePage() {
  const shareBtn = $("shareBtn");
  const shareDownloadBtn = $("shareDownloadBtn");
  const shareRefreshBtn = $("shareRefreshBtn");
  const shareSelected = $("shareSelected");
  if (shareBtn) shareBtn.addEventListener("click", handleShareCreate);
  if (shareDownloadBtn) shareDownloadBtn.addEventListener("click", handleShareDownload);
  if (shareRefreshBtn) {
    shareRefreshBtn.addEventListener("click", () => renderStoredSelection(shareSelected));
  }
  renderStoredSelection(shareSelected);
}

async function handleRecycleList() {
  const status = $("recycleStatus");
  const rows = $("recycleRows");
  try {
    setStatus(status, "正在加载回收站...");
    const data = await apiFetch("/recycle/list", { method: "POST" });
    const files = data.files || [];
    state.recycleFiles = files;
    state.recycleSelected.clear();
    updateRecycleSelectionSummary();
    const selectAll = $("recycleSelectAll");
    if (selectAll) selectAll.checked = false;
    renderRecycle(rows, files);
    setStatus(status, `Loaded ${files.length} items.`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function renderRecycle(container, files) {
  if (!container) return;
  container.innerHTML = "";
  files.forEach((file) => {
    const row = document.createElement("div");
    row.className = "row";
    row.dataset.id = String(file.id);

    const checkCell = document.createElement("span");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.checked = state.recycleSelected.has(file.id);
    checkbox.addEventListener("change", () => {
      if (checkbox.checked) {
        state.recycleSelected.set(file.id, file);
      } else {
        state.recycleSelected.delete(file.id);
      }
      row.classList.toggle("selected", checkbox.checked);
      const selectAll = $("recycleSelectAll");
      if (selectAll) {
        selectAll.checked =
            state.recycleSelected.size === state.recycleFiles.length && state.recycleFiles.length > 0;
      }
      updateRecycleSelectionSummary();
    });
    checkCell.appendChild(checkbox);

    const nameCell = document.createElement("span");
    nameCell.textContent = file.name;

    const typeCell = document.createElement("span");
    typeCell.textContent = file.is_dir ? "Folder" : "File";

    const sizeCell = document.createElement("span");
    sizeCell.textContent = file.is_dir ? "-" : formatSize(file.size);

    const deletedCell = document.createElement("span");
    deletedCell.textContent = formatDate(file.deleted_at || file.deletedAt || file.DeletedAt);

    row.appendChild(checkCell);
    row.appendChild(nameCell);
    row.appendChild(typeCell);
    row.appendChild(sizeCell);
    row.appendChild(deletedCell);

    if (state.recycleSelected.has(file.id)) {
      row.classList.add("selected");
    }
    container.appendChild(row);
  });
}

async function handleRecycleRestore() {
  const status = $("recycleActionStatus");
  try {
    const items = Array.from(state.recycleSelected.values());
    if (items.length === 0) {
      setStatus(status, "Please select at least one item.", true);
      return;
    }
    setStatus(status, "正在恢复...");
    for (const item of items) {
      await apiFetch("/recycle/restore", {
        method: "POST",
        body: JSON.stringify({ file_id: item.id }),
      });
    }
    setStatus(status, `Restored ${items.length} items.`);
    handleRecycleList();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function handleRecycleDelete() {
  const status = $("recycleActionStatus");
  try {
    const items = Array.from(state.recycleSelected.values());
    if (items.length === 0) {
      setStatus(status, "Please select at least one item.", true);
      return;
    }
    setStatus(status, "正在删除...");
    for (const item of items) {
      await apiFetch("/recycle/delete", {
        method: "POST",
        body: JSON.stringify({ file_id: item.id }),
      });
    }
    setStatus(status, `Deleted ${items.length} items.`);
    handleRecycleList();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initRecyclePage() {
  const listBtn = $("recycleListBtn");
  const restoreBtn = $("recycleRestoreBtn");
  const deleteBtn = $("recycleDeleteBtn");
  const selectAll = $("recycleSelectAll");
  if (listBtn) listBtn.addEventListener("click", handleRecycleList);
  if (restoreBtn) restoreBtn.addEventListener("click", handleRecycleRestore);
  if (deleteBtn) deleteBtn.addEventListener("click", handleRecycleDelete);
  if (selectAll) {
    selectAll.addEventListener("change", (event) => {
      toggleRecycleSelectAll(event.target.checked);
    });
  }
}

async function handleTaskCreate() {
  const status = $("taskStatus");
  try {
    const rawURL = $("taskUrl")?.value.trim() || "";
    if (!rawURL) {
      setStatus(status, "URL is required.", true);
      return;
    }
    const taskNameInput = $("taskName");
    let taskName = taskNameInput ? taskNameInput.value.trim() : "";
    if (!taskName) {
      taskName = guessFilenameFromURL(rawURL);
      if (taskNameInput) taskNameInput.value = taskName;
    }
    setStatus(status, "正在创建任务...");
    const data = await apiFetch("/file/download/offline", {
      method: "POST",
      body: JSON.stringify({
        url: rawURL,
        file_name: taskName,
      }),
    });
    const taskID = data.task_id ?? data.taskID ?? data.id ?? "-";
    setStatus(status, `任务已创建：${taskID}`);
    handleTaskList();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function guessFilenameFromURL(rawURL) {
  try {
    const parsed = new URL(rawURL);
    const parts = parsed.pathname.split("/").filter(Boolean);
    if (parts.length > 0) {
      const name = decodeURIComponent(parts[parts.length - 1]).trim();
      if (name) return name;
    }
  } catch (err) {
    // ignore parse failure and use fallback
  }
  return `offline-${Date.now()}`;
}

function formatDate(value) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function renderTasks(container, tasks) {
  if (!container) return;
  container.innerHTML = "";
  tasks.forEach((task) => {
    const id = task.id ?? task.ID ?? "-";
    const status = task.status ?? task.Status ?? "-";
    const progress = Number(task.progress ?? task.Progress ?? 0);
    const retry = task.retry_count ?? task.RetryCount ?? 0;
    const source = task.source ?? task.Source ?? "-";
    const errorMsg = task.error_msg ?? task.ErrorMsg ?? "-";
    const updated = task.updated_at || task.updatedAt || task.UpdatedAt;
    const row = document.createElement("div");
    row.className = "row";
    row.innerHTML = `
      <span>${id}</span>
      <span>${status}</span>
      <span>${progress}%</span>
      <span>${retry}</span>
      <span>${formatDate(updated)}</span>
      <span>${source}</span>
      <span title="${String(errorMsg).replace(/"/g, "&quot;")}">${errorMsg}</span>
    `;
    container.appendChild(row);
  });
}

async function handleTaskList() {
  const status = $("taskListStatus");
  const rows = $("taskRows");
  try {
    setStatus(status, "正在加载任务...");
    const data = await apiFetch("/file/download/tasks", { method: "GET" });
    renderTasks(rows, data.tasks || []);
    setStatus(status, "Task list refreshed.");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initTasksPage() {
  const createBtn = $("taskCreateBtn");
  const listBtn = $("taskListBtn");
  if (createBtn) createBtn.addEventListener("click", handleTaskCreate);
  if (listBtn) listBtn.addEventListener("click", handleTaskList);
  handleTaskList();
}

function initPreviewPage() {
  const btn = $("previewDirectBtn");
  const refreshBtn = $("previewRefreshBtn");
  const previewSelected = $("previewSelected");
  if (btn) btn.addEventListener("click", handlePreviewLink);
  if (refreshBtn) {
    refreshBtn.addEventListener("click", () => renderStoredSelection(previewSelected));
  }
  renderStoredSelection(previewSelected);
}

function formatDateTime(value) {
  if (!value && value !== 0) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString();
}

function formatExpireIn(expireAt) {
  if (!expireAt || Number.isNaN(expireAt.getTime())) return "-";
  const diff = expireAt.getTime() - Date.now();
  if (diff <= 0) return "已过期";
  const minutes = Math.floor(diff / 60000);
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor((minutes % 1440) / 60);
  const mins = minutes % 60;
  if (days > 0) return `${days}天${hours}小时`;
  if (hours > 0) return `${hours}小时${mins}分钟`;
  return `${Math.max(1, mins)}分钟`;
}

function renderProfileIdentity() {
  const payload = decodeJWT(state.token);
  const profile = state.userProfile;
  const userId = $("profileUserId");
  const userName = $("profileUserName");
  const nickName = $("profileNickName");
  const email = $("profileEmail");
  const active = $("profileActive");
  const totalSpace = $("profileTotalSpace");
  const useSpace = $("profileUseSpace");
  const issuedAt = $("profileIssuedAt");
  const expireAt = $("profileExpireAt");
  const expireIn = $("profileExpireIn");
  const apiBase = $("profileApiBase");

  if (apiBase) {
    apiBase.textContent = getApiBase();
  }

  if (!payload) {
    if (userId) userId.textContent = "-";
    if (userName) userName.textContent = "-";
    if (nickName) nickName.textContent = "-";
    if (email) email.textContent = "-";
    if (active) active.textContent = "未登录";
    if (totalSpace) totalSpace.textContent = "-";
    if (useSpace) useSpace.textContent = "-";
    if (issuedAt) issuedAt.textContent = "-";
    if (expireAt) expireAt.textContent = "-";
    if (expireIn) expireIn.textContent = "-";
    return;
  }

  const id = profile?.id || payload.user_id || "-";
  const name = profile?.username || payload.username || "-";
  const profileNickName = profile?.nickName || "-";
  const profileEmail = profile?.email || "-";
  const isActive = profile ? (profile.isActive ? "已激活" : "未激活") : "未知";
  const iat = payload.iat ? new Date(payload.iat * 1000) : null;
  const exp = payload.exp ? new Date(payload.exp * 1000) : null;

  if (userId) userId.textContent = String(id);
  if (userName) userName.textContent = name;
  if (nickName) nickName.textContent = profileNickName;
  if (email) email.textContent = profileEmail;
  if (active) active.textContent = isActive;
  if (totalSpace) totalSpace.textContent = formatSize(profile?.totalSpace ?? 0);
  if (useSpace) useSpace.textContent = formatSize(profile?.useSpace ?? 0);
  if (issuedAt) issuedAt.textContent = iat ? formatDateTime(iat) : "-";
  if (expireAt) expireAt.textContent = exp ? formatDateTime(exp) : "-";
  if (expireIn) expireIn.textContent = exp ? formatExpireIn(exp) : "-";
}

function applyProfileForm(profile) {
  const nickName = $("profileEditNickName");
  const email = $("profileEditEmail");
  const avatar = $("profileEditAvatar");
  const bio = $("profileEditBio");
  if (nickName) nickName.value = profile?.nickName || "";
  if (email) email.value = profile?.email || "";
  if (avatar) avatar.value = profile?.avatarURL || "";
  if (bio) bio.value = profile?.bio || "";
}

async function loadCurrentUserProfile({ silent = false } = {}) {
  const status = $("profileStatus");
  if (!state.token) {
    if (status) setStatus(status, "请先登录后查看个人资料。", true);
    return;
  }
  try {
    if (status && !silent) {
      setStatus(status, "正在加载个人资料...");
    }
    const data = await apiFetch("/user/me", { method: "GET" });
    setUserProfile(data);
    renderProfileIdentity();
    applyProfileForm(state.userProfile);
    if (status && !silent) {
      setStatus(status, "资料已刷新。");
    }
  } catch (err) {
    if (status) setStatus(status, err.message, true);
  }
}

async function saveCurrentUserProfile() {
  const status = $("profileSaveStatus");
  if (!state.token) {
    setStatus(status, "请先登录。", true);
    return;
  }
  try {
    setStatus(status, "正在保存...");
    const payload = {
      nick_name: $("profileEditNickName")?.value.trim() || "",
      email: $("profileEditEmail")?.value.trim() || "",
      avatar_url: $("profileEditAvatar")?.value.trim() || "",
      bio: $("profileEditBio")?.value.trim() || "",
    };
    const data = await apiFetch("/user/me", {
      method: "PUT",
      body: JSON.stringify(payload),
    });
    setUserProfile(data);
    renderProfileIdentity();
    applyProfileForm(state.userProfile);
    setStatus(status, "保存成功。");
    const mainStatus = $("profileStatus");
    if (mainStatus) {
      setStatus(mainStatus, "资料已更新。");
    }
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function renderActivityRows(items = []) {
  const rows = $("activityRows");
  if (!rows) return;
  rows.innerHTML = "";
  items.forEach((item) => {
    const row = document.createElement("div");
    row.className = "row";
    row.innerHTML = `
      <span>${item.date || "-"}</span>
      <span>${item.upload_count || 0} / ${formatSize(item.upload_bytes || 0)}</span>
      <span>${item.download_count || 0} / ${formatSize(item.download_bytes || 0)}</span>
      <span>${item.delete_count || 0} / ${formatSize(item.delete_bytes || 0)}</span>
      <span>${item.share_count || 0}</span>
      <span>${formatSize((item.upload_bytes || 0) + (item.download_bytes || 0))}</span>
    `;
    rows.appendChild(row);
  });
}

async function loadProfileActivitySummary(days = 7) {
  const status = $("activityStatus");
  if (!state.token) {
    setStatus(status, "请先登录后查看活动统计。", true);
    return;
  }
  try {
    setStatus(status, "正在加载活动统计...");
    const data = await apiFetch(`/user/activity/summary?days=${encodeURIComponent(days)}`, {
      method: "GET",
    });
    renderActivityRows(data.items || []);

    const setText = (id, value) => {
      const el = $(id);
      if (el) el.textContent = value;
    };
    setText("totalUploadCount", String(data.total_upload_count || 0));
    setText("totalUploadBytes", formatSize(data.total_upload_bytes || 0));
    setText("totalDeleteCount", String(data.total_delete_count || 0));
    setText("totalDownloadCount", String(data.total_download_count || 0));
    setText("totalDownloadBytes", formatSize(data.total_download_bytes || 0));
    setText("totalShareCount", String(data.total_share_count || 0));
    setStatus(status, `已加载最近 ${data.days || days} 天统计。`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initProfilePage() {
  renderProfileIdentity();
  const status = $("profileStatus");
  const refreshBtn = $("profileRefreshBtn");
  const reloadBtn = $("profileReloadBtn");
  const saveBtn = $("profileSaveBtn");
  const loadBtn = $("activityLoadBtn");
  const daySelect = $("activityDays");

  if (refreshBtn) {
    refreshBtn.addEventListener("click", () => {
      const days = Number(daySelect?.value || 7);
      loadProfileActivitySummary(days);
      if (status) {
        setStatus(status, "统计已刷新。");
      }
    });
  }
  if (reloadBtn) {
    reloadBtn.addEventListener("click", () => {
      loadCurrentUserProfile();
    });
  }
  if (saveBtn) {
    saveBtn.addEventListener("click", saveCurrentUserProfile);
  }

  if (loadBtn) {
    loadBtn.addEventListener("click", () => {
      const days = Number(daySelect?.value || 7);
      loadProfileActivitySummary(days);
    });
  }

  applyProfileForm(state.userProfile);
  loadCurrentUserProfile({ silent: true });
  const days = Number(daySelect?.value || 7);
  loadProfileActivitySummary(days);
}

function renderGridRows(container, columns, rows) {
  if (!container) return;
  container.innerHTML = "";
  if (!rows || rows.length === 0) {
    const row = document.createElement("div");
    row.className = "row";
    for (let i = 0; i < columns; i += 1) {
      const span = document.createElement("span");
      span.textContent = i === 0 ? "暂无数据" : "";
      row.appendChild(span);
    }
    container.appendChild(row);
    return;
  }
  rows.forEach((cells) => {
    const row = document.createElement("div");
    row.className = "row";
    cells.forEach((value) => {
      const span = document.createElement("span");
      span.textContent = value ?? "-";
      row.appendChild(span);
    });
    container.appendChild(row);
  });
}

function renderFavoriteRows(items = []) {
  const rows = $("favoriteRows");
  if (!rows) return;
  rows.innerHTML = "";
  if (items.length === 0) {
    renderGridRows(rows, 6, []);
    return;
  }
  items.forEach((item) => {
    const row = document.createElement("div");
    row.className = "row";
    const values = [
      String(item.file_id ?? "-"),
      item.name || "-",
      item.is_dir ? "目录" : "文件",
      item.is_dir ? "-" : formatSize(item.size || 0),
      formatDate(item.created_at),
    ];
    values.forEach((value) => {
      const span = document.createElement("span");
      span.textContent = value;
      row.appendChild(span);
    });

    const action = document.createElement("span");
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "ghost mini-btn";
    btn.textContent = "取消收藏";
    btn.addEventListener("click", async () => {
      const status = $("favoriteStatus");
      try {
        await apiFetch(`/user/favorites/${item.file_id}`, { method: "DELETE" });
        setStatus(status, `已取消收藏 #${item.file_id}`);
        loadFavorites();
      } catch (err) {
        setStatus(status, err.message, true);
      }
    });
    action.appendChild(btn);
    row.appendChild(action);
    rows.appendChild(row);
  });
}

async function loadFavorites() {
  const status = $("favoriteStatus");
  if (!state.token) {
    setStatus(status, "请先登录。", true);
    return;
  }
  try {
    setStatus(status, "正在加载收藏...");
    const data = await apiFetch("/user/favorites?limit=100", { method: "GET" });
    renderFavoriteRows(data.items || []);
    setStatus(status, `已加载 ${data.items?.length || 0} 条收藏。`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function addFavoriteFromSelection() {
  const status = $("favoriteStatus");
  const selection = resolveSingleSelection({ allowStored: true });
  if (selection.error) {
    setStatus(status, selection.error, true);
    return;
  }
  try {
    setStatus(status, "正在添加收藏...");
    await apiFetch("/user/favorites", {
      method: "POST",
      body: JSON.stringify({ file_id: selection.item.id }),
    });
    setStatus(status, `收藏成功：${selection.item.name}`);
    loadFavorites();
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function renderRecentRows(items = []) {
  const rows = $("recentRows");
  if (!rows) return;
  const mapped = items.map((item) => [
    String(item.file_id ?? "-"),
    item.name || "-",
    item.source || "-",
    String(item.access_count || 0),
    formatDate(item.last_access_at),
  ]);
  renderGridRows(rows, 5, mapped);
}

async function loadRecent() {
  const status = $("recentStatus");
  if (!state.token) {
    setStatus(status, "请先登录。", true);
    return;
  }
  try {
    setStatus(status, "正在加载最近访问...");
    const data = await apiFetch("/user/recent?limit=100", { method: "GET" });
    renderRecentRows(data.items || []);
    setStatus(status, `已加载 ${data.items?.length || 0} 条最近访问。`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function renderCommonDirRows(items = []) {
  const rows = $("commonDirsRows");
  if (!rows) return;
  const mapped = items.map((item) => [
    String(item.file_id ?? "-"),
    item.name || "-",
    String(item.access_count || 0),
    formatDate(item.last_access_at),
  ]);
  renderGridRows(rows, 4, mapped);
}

async function loadCommonDirs() {
  const status = $("commonDirsStatus");
  if (!state.token) {
    setStatus(status, "请先登录。", true);
    return;
  }
  try {
    setStatus(status, "正在加载常用目录...");
    const data = await apiFetch("/user/common-dirs?limit=30", { method: "GET" });
    renderCommonDirRows(data.items || []);
    setStatus(status, `已加载 ${data.items?.length || 0} 个目录。`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initLibraryPage() {
  const selected = $("librarySelected");
  const refreshSelectionBtn = $("libraryRefreshSelectionBtn");
  const addFavoriteBtn = $("favoriteAddBtn");
  const favoriteListBtn = $("favoriteListBtn");
  const recentListBtn = $("recentListBtn");
  const commonDirsBtn = $("commonDirsBtn");

  renderStoredSelection(selected);
  if (refreshSelectionBtn) {
    refreshSelectionBtn.addEventListener("click", () => renderStoredSelection(selected));
  }
  if (addFavoriteBtn) addFavoriteBtn.addEventListener("click", addFavoriteFromSelection);
  if (favoriteListBtn) favoriteListBtn.addEventListener("click", loadFavorites);
  if (recentListBtn) recentListBtn.addEventListener("click", loadRecent);
  if (commonDirsBtn) commonDirsBtn.addEventListener("click", loadCommonDirs);

  loadFavorites();
  loadRecent();
  loadCommonDirs();
}

function renderShareSourceRows(items = []) {
  const rows = $("shareSourceRows");
  const mapped = items.map((item) => [item.source || "-", String(item.count || 0)]);
  renderGridRows(rows, 2, mapped);
}

function renderShareDailyRows(items = []) {
  const rows = $("shareDailyRows");
  const mapped = items.map((item) => [item.date || "-", String(item.count || 0)]);
  renderGridRows(rows, 2, mapped);
}

function renderShareTopRows(items = []) {
  const rows = $("shareTopRows");
  const mapped = items.map((item) => [
    item.share_id || "-",
    String(item.file_id || "-"),
    item.file_name || "-",
    String(item.count || 0),
  ]);
  renderGridRows(rows, 4, mapped);
}

function renderShareLogRows(items = []) {
  const rows = $("shareLogRows");
  const mapped = items.map((item) => [
    formatDate(item.accessed_at),
    item.share_id || "-",
    `${item.file_name || "-"} (#${item.file_id || "-"})`,
    item.source || "-",
    item.visitor_ip || "-",
    item.referer || "-",
  ]);
  renderGridRows(rows, 6, mapped);
}

async function loadShareStats() {
  const status = $("shareStatsStatus");
  if (!state.token) {
    setStatus(status, "请先登录。", true);
    return;
  }
  const days = Number($("shareStatsDays")?.value || 30);
  try {
    setStatus(status, "正在加载分享统计...");
    const data = await apiFetch(`/share/access/stats?days=${encodeURIComponent(days)}`, {
      method: "GET",
    });

    const setText = (id, value) => {
      const el = $(id);
      if (el) el.textContent = value;
    };
    setText("shareVisitTotal", String(data.total_visits || 0));
    setText("shareVisitUniqueIPs", String(data.unique_ips || 0));
    setText("shareVisitWindow", `${data.days || days} 天`);

    renderShareSourceRows(data.by_source || []);
    renderShareDailyRows(data.daily || []);
    renderShareTopRows(data.top_shares || []);
    setStatus(status, "分享统计已刷新。");
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

async function loadShareLogs() {
  const status = $("shareLogStatus");
  if (!state.token) {
    setStatus(status, "请先登录。", true);
    return;
  }
  const limit = Number($("shareLogLimit")?.value || 50);
  const shareID = $("shareLogShareId")?.value.trim() || "";
  try {
    setStatus(status, "正在加载访问日志...");
    let query = `?limit=${encodeURIComponent(limit > 0 ? limit : 50)}`;
    if (shareID) {
      query += `&share_id=${encodeURIComponent(shareID)}`;
    }
    const data = await apiFetch(`/share/access/logs${query}`, { method: "GET" });
    renderShareLogRows(data.items || []);
    setStatus(status, `已加载 ${data.items?.length || 0} 条访问日志。`);
  } catch (err) {
    setStatus(status, err.message, true);
  }
}

function initShareAnalyticsPage() {
  const statsBtn = $("shareStatsLoadBtn");
  const logsBtn = $("shareLogLoadBtn");
  if (statsBtn) statsBtn.addEventListener("click", loadShareStats);
  if (logsBtn) logsBtn.addEventListener("click", loadShareLogs);
  loadShareStats();
  loadShareLogs();
}

function initHomePage() {
  const pingBtn = $("pingBtn");
  if (!pingBtn) return;
  pingBtn.addEventListener("click", async () => {
    const status = $("pingStatus");
    try {
      setStatus(status, "正在验证...", false);
      if (!state.token) {
        setStatus(status, "Token not found.", true);
        return;
      }
      await apiFetch("/file/download/tasks", { method: "GET" });
      setStatus(status, "Current token can access API.");
    } catch (err) {
      setStatus(status, err.message, true);
    }
  });
}

function initPage() {
  // Ensure old localStorage token never auto-restores login.
  localStorage.removeItem(TOKEN_STORAGE_KEY);
  initBaseControls();
  const page = document.body.dataset.page;
  const map = {
    home: initHomePage,
    auth: initAuthPage,
    files: initFilesPage,
    upload: initUploadPage,
    share: initSharePage,
    recycle: initRecyclePage,
    tasks: initTasksPage,
    profile: initProfilePage,
    library: initLibraryPage,
    "share-analytics": initShareAnalyticsPage,
    preview: initPreviewPage,
  };
  if (map[page]) {
    map[page]();
  }
}

initPage();



