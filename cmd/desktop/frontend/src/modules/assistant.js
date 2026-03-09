import { t } from '../i18n/index.js';

// 状态管理
let currentProduct = 'claude';  // claude/codex/gemini
let currentFileKey = 'md';      // md/settings/config
let currentModTime = 0;
let isModified = false;
let debounceTimer = null;

// 渲染助手设置视图
export function renderAssistantView() {
    const container = document.getElementById('assistant-view');
    if (!container) return;

    container.innerHTML = `
        <div class="assistant-container">
            <h2 class="assistant-title">${t('assistant.title')}</h2>

            <div class="file-selector assistant-file-selector" style="max-width: 300px;">
                <button class="file-btn active" data-file="md">${t('assistant.markdown')}</button>
                <button class="file-btn dynamic-file-btn" data-file="settings">${t('assistant.settings')}</button>
            </div>

            <!-- 单栏编辑器 -->
            <div class="editor-container">
                <div class="editor-toolbar">
                    <span id="file-path"></span>
                    <button id="reload-btn" style="display: none;">
                        🔄 ${t('assistant.reload')}
                    </button>
                </div>
                <textarea id="config-editor" class="log-textarea" placeholder="${t('assistant.loading')}"></textarea>
                <div id="error-hint" class="error-hint" style="display: none;"></div>
                <div class="editor-actions">
                    <button id="create-btn" class="secondary-btn" style="display: none;">
                        ${t('assistant.create')}
                    </button>
                    <div style="flex: 1;"></div>
                    <button id="save-btn" class="primary-btn" disabled>
                        ${t('assistant.save')}
                    </button>
                </div>
            </div>
        </div>
    `;

    // 绑定事件
    bindAssistantEvents();
}

// 绑定事件监听器
function bindAssistantEvents() {
    // 侧边栏子菜单切换（只监听 assistant-submenu 内的子项）
    document.querySelectorAll('#assistant-submenu .nav-subitem').forEach(item => {
        item.addEventListener('click', () => {
            const product = item.dataset.product;
            switchProduct(product);
        });
    });

    // 文件切换
    document.querySelectorAll('.file-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            // 检查是否有未保存的更改
            if (isModified && !confirm(t('assistant.unsavedChanges'))) {
                return;
            }

            const fileKey = btn.dataset.file;
            switchFile(fileKey);
        });
    });

    // 编辑器输入
    document.getElementById('config-editor')?.addEventListener('input', handleEditorInput);

    // 保存按钮
    document.getElementById('save-btn')?.addEventListener('click', saveConfig);

    // 创建按钮
    document.getElementById('create-btn')?.addEventListener('click', createConfig);

    // 重新加载按钮
    document.getElementById('reload-btn')?.addEventListener('click', () => loadConfig(currentProduct, currentFileKey));

    // 初始加载
    loadConfig(currentProduct, currentFileKey);
}

// 切换产品
function switchProduct(product) {
    currentProduct = product;

    // 更新侧边栏子菜单样式（只更新 assistant-submenu 内的子项）
    document.querySelectorAll('#assistant-submenu .nav-subitem').forEach(item => {
        const isActive = item.dataset.product === product;
        item.classList.toggle('active', isActive);
    });

    // 修复 Bug：使用更稳定的类名选择器
    const settingsBtn = document.querySelector('.dynamic-file-btn');
    if (product === 'codex') {
        if (settingsBtn) {
            settingsBtn.dataset.file = 'config';
            settingsBtn.textContent = t('assistant.config');
        }
        if (currentFileKey === 'settings') {
            currentFileKey = 'config';
        }
    } else {
        if (settingsBtn) {
            settingsBtn.dataset.file = 'settings';
            settingsBtn.textContent = t('assistant.settings');
        }
        if (currentFileKey === 'config') {
            currentFileKey = 'settings';
        }
    }

    // 更新文件按钮 active 状态
    document.querySelectorAll('.file-btn').forEach(btn => {
        const isActive = btn.dataset.file === currentFileKey;
        btn.classList.toggle('active', isActive);
    });

    loadConfig(currentProduct, currentFileKey);
}

// 切换文件
function switchFile(fileKey) {
    currentFileKey = fileKey;

    // 更新按钮样式
    document.querySelectorAll('.file-btn').forEach(btn => {
        const isActive = btn.dataset.file === fileKey;
        btn.classList.toggle('active', isActive);
    });

    loadConfig(currentProduct, currentFileKey);
}

// 编辑器输入处理
function handleEditorInput(e) {
    isModified = true;
    const saveBtn = document.getElementById('save-btn');
    const errorHint = document.getElementById('error-hint');

    // Debounce JSON 校验
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
        const isJSON = (currentFileKey === 'settings' || currentFileKey === 'config' && currentProduct !== 'codex');

        if (isJSON && currentFileKey === 'settings') {
            try {
                JSON.parse(e.target.value);
                errorHint.style.display = 'none';
                if (saveBtn) {
                    saveBtn.disabled = false;
                    saveBtn.style.opacity = '1';
                }
            } catch (err) {
                errorHint.textContent = `${t('assistant.jsonError')}: ${err.message}`;
                errorHint.style.display = 'block';
                if (saveBtn) {
                    saveBtn.disabled = true;
                    saveBtn.style.opacity = '0.5';
                }
            }
        } else {
            errorHint.style.display = 'none';
            if (saveBtn) {
                saveBtn.disabled = false;
                saveBtn.style.opacity = '1';
            }
        }
    }, 500);
}

// 加载配置
async function loadConfig(product, fileKey) {
    const editor = document.getElementById('config-editor');
    const errorHint = document.getElementById('error-hint');
    const createBtn = document.getElementById('create-btn');
    const saveBtn = document.getElementById('save-btn');
    const filePath = document.getElementById('file-path');

    if (!editor) return;

    // 清空编辑器
    editor.value = '';
    editor.placeholder = t('assistant.loading');
    errorHint.style.display = 'none';
    createBtn.style.display = 'none';
    if (saveBtn) {
        saveBtn.disabled = true;
        saveBtn.style.opacity = '0.5';
    }

    try {
        const result = JSON.parse(await window.go.main.App.ReadAIConfig(product, fileKey));

        if (result.ok) {
            editor.value = result.content;
            editor.placeholder = '';
            currentModTime = result.modTime;
            filePath.textContent = result.path;
            isModified = false;
        } else if (result.code === 'NotExist') {
            editor.placeholder = t('assistant.fileNotFound');
            errorHint.textContent = `${t('assistant.fileNotFound')}: ${result.path}`;
            errorHint.style.display = 'block';
            createBtn.style.display = 'inline-block';
            filePath.textContent = result.path;
        } else if (result.code === 'PermissionDenied') {
            editor.placeholder = t('assistant.permissionDenied');
            errorHint.textContent = `${t('assistant.permissionDenied')}: ${result.path}`;
            errorHint.style.display = 'block';
            filePath.textContent = result.path;
        } else {
            editor.placeholder = t('assistant.loadFailed');
            errorHint.textContent = result.message || t('assistant.loadFailed');
            errorHint.style.display = 'block';
        }
    } catch (err) {
        editor.placeholder = t('assistant.error');
        errorHint.textContent = `${t('assistant.error')}: ${err.message}`;
        errorHint.style.display = 'block';
    }
}

// 保存配置
async function saveConfig() {
    const editor = document.getElementById('config-editor');
    const saveBtn = document.getElementById('save-btn');
    const errorHint = document.getElementById('error-hint');

    if (!editor) return;

    const content = editor.value;
    if (saveBtn) {
        saveBtn.disabled = true;
        saveBtn.textContent = t('assistant.saving');
    }

    try {
        const result = JSON.parse(await window.go.main.App.WriteAIConfig(
            currentProduct, currentFileKey, content, currentModTime
        ));

        if (result.ok) {
            currentModTime = result.modTime;
            isModified = false;
            showToast(t('assistant.saveSuccess'));
            if (saveBtn) {
                saveBtn.style.opacity = '0.5';
            }
        } else if (result.code === 'Conflict') {
            if (confirm(t('assistant.conflictConfirm'))) {
                // 用户选择覆盖，重新保存（expectedModTime = 0）
                const retryResult = JSON.parse(await window.go.main.App.WriteAIConfig(
                    currentProduct, currentFileKey, content, 0
                ));
                if (retryResult.ok) {
                    currentModTime = retryResult.modTime;
                    isModified = false;
                    showToast(t('assistant.saveSuccess'));
                    if (saveBtn) {
                        saveBtn.style.opacity = '0.5';
                    }
                } else {
                    errorHint.textContent = retryResult.message;
                    errorHint.style.display = 'block';
                }
            }
        } else if (result.code === 'InvalidJSON') {
            errorHint.textContent = t('assistant.invalidJSON');
            errorHint.style.display = 'block';
        } else {
            errorHint.textContent = result.message || t('assistant.saveFailed');
            errorHint.style.display = 'block';
        }
    } catch (err) {
        errorHint.textContent = `${t('assistant.error')}: ${err.message}`;
        errorHint.style.display = 'block';
    } finally {
        if (saveBtn) {
            saveBtn.disabled = false;
            saveBtn.textContent = t('assistant.save');
        }
    }
}

// 创建配置
async function createConfig() {
    const createBtn = document.getElementById('create-btn');
    const errorHint = document.getElementById('error-hint');

    if (!createBtn) return;

    createBtn.disabled = true;
    createBtn.textContent = t('assistant.creating');

    try {
        const initialContent = (currentFileKey === 'settings') ? '{}' : '';
        const result = JSON.parse(await window.go.main.App.CreateAIConfig(
            currentProduct, currentFileKey, initialContent
        ));

        if (result.ok) {
            showToast(t('assistant.createSuccess'));
            await loadConfig(currentProduct, currentFileKey);
        } else if (result.code === 'AlreadyExists') {
            errorHint.textContent = t('assistant.fileExists');
            errorHint.style.display = 'block';
        } else {
            errorHint.textContent = result.message || t('assistant.createFailed');
            errorHint.style.display = 'block';
        }
    } catch (err) {
        errorHint.textContent = `${t('assistant.error')}: ${err.message}`;
        errorHint.style.display = 'block';
    } finally {
        createBtn.disabled = false;
        createBtn.textContent = t('assistant.create');
    }
}

// Toast 提示
function showToast(message) {
    // 复用现有的 toast 机制（如果有）
    const toast = document.createElement('div');
    toast.textContent = message;
    toast.style.cssText = `
        position: fixed;
        top: 20px;
        right: 20px;
        padding: 12px 24px;
        background: var(--primary-color);
        color: white;
        border-radius: 4px;
        box-shadow: 0 2px 8px rgba(0,0,0,0.2);
        z-index: 10000;
        animation: slideIn 0.3s ease;
    `;
    document.body.appendChild(toast);

    setTimeout(() => {
        toast.style.animation = 'slideOut 0.3s ease';
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

// 初始化函数
export function initAssistantView() {
    renderAssistantView();
}
