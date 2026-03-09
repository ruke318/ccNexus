import { t } from '../i18n/index.js';

let currentSkillDir = null;
let currentSkillContent = '';
let currentSkillModTime = 0;
let isEditMode = false;

// 初始化 Skills 视图
export function initSkillsView() {
    renderSkillsView();
    renderSkillsSubmenu();
}

// 渲染 Skills 视图结构（全屏编辑器）
function renderSkillsView() {
    const container = document.getElementById('skills-view');
    if (!container) return;

    container.innerHTML = `
        <div class="assistant-container">
            <h2 class="assistant-title">${t('menu.skills')}</h2>

            <!-- 直接编辑器 -->
            <div class="editor-container">
                <div class="editor-toolbar">
                    <span id="current-skill-name" class="skill-name">-</span>
                </div>
                <textarea id="skills-editor" class="log-textarea" placeholder="${t('skills.selectDirectory')}"></textarea>
                <div id="skills-error-hint" class="error-hint" style="display: none;"></div>
                <div class="editor-actions">
                    <div style="flex: 1;"></div>
                    <button id="skill-save-btn" class="primary-btn">${t('common.save')}</button>
                    <button id="skill-cancel-btn" class="secondary-btn">${t('common.cancel')}</button>
                </div>
            </div>
        </div>
    `;

    bindSkillsEvents();
}

// 渲染 Skills 侧边栏子菜单
export async function renderSkillsSubmenu() {
    const submenu = document.getElementById('skills-submenu');
    if (!submenu) return;

    try {
        const result = await window.go.main.App.ListSkillDirs();
        const data = JSON.parse(result);

        if (!data.ok) {
            submenu.innerHTML = `<div class="error" style="padding: 10px 20px; font-size: 12px; color: var(--text-tertiary);">${data.message}</div>`;
            return;
        }

        const dirs = data.files || [];

        if (dirs.length === 0) {
            submenu.innerHTML = `<div class="empty-state" style="padding: 10px 20px; font-size: 12px; color: var(--text-tertiary);">${t('skills.emptyState')}</div>`;
            return;
        }

        // 渲染子菜单项
        submenu.innerHTML = dirs.map((dir, index) => `
            <div class="nav-subitem ${index === 0 && !currentSkillDir ? 'active' : (currentSkillDir === dir.relPath ? 'active' : '')}" data-dir="${dir.relPath}">
                <span>${dir.relPath}</span>
            </div>
        `).join('');

        // 绑定点击事件
        submenu.querySelectorAll('.nav-subitem').forEach(item => {
            item.addEventListener('click', () => {
                const dirName = item.dataset.dir;
                selectDirectory(dirName);
            });
        });

        // 自动加载第一个目录（如果没有当前选择）
        if (dirs.length > 0 && !currentSkillDir) {
            loadSkillDoc(dirs[0].relPath);
        } else if (currentSkillDir) {
            loadSkillDoc(currentSkillDir);
        }
    } catch (error) {
        console.error('Error loading skill directories:', error);
        submenu.innerHTML = `<div class="error" style="padding: 10px 20px; font-size: 12px; color: var(--text-tertiary);">${t('skills.loadError')}</div>`;
    }
}

// 绑定事件
function bindSkillsEvents() {
    const saveBtn = document.getElementById('skill-save-btn');
    const cancelBtn = document.getElementById('skill-cancel-btn');

    if (saveBtn) {
        saveBtn.addEventListener('click', saveSkillDoc);
    }

    if (cancelBtn) {
        cancelBtn.addEventListener('click', () => {
            // 取消编辑，重新加载文档
            if (currentSkillDir) {
                loadSkillDoc(currentSkillDir);
            }
        });
    }
}

// 选择目录
function selectDirectory(dirName) {
    // 更新 active 状态
    document.querySelectorAll('#skills-submenu .nav-subitem').forEach(item => {
        item.classList.toggle('active', item.dataset.dir === dirName);
    });

    // 加载文档
    loadSkillDoc(dirName);
}

// 加载 SKILL.md
async function loadSkillDoc(dirName) {
    const editor = document.getElementById('skills-editor');
    const skillName = document.getElementById('current-skill-name');

    if (!editor) return; // 可能视图还没渲染

    editor.value = '';
    editor.placeholder = t('common.loading') || '加载中...';

    try {
        const result = await window.go.main.App.ReadSkillDoc(dirName);
        const data = JSON.parse(result);

        if (skillName) skillName.textContent = dirName;

        if (!data.ok) {
            if (data.code === 'NotExist') {
                editor.placeholder = t('skills.skillMdNotFound') || 'SKILL.md 不存在，开始编辑以创建';
                editor.value = '';
            } else {
                editor.placeholder = `${t('skills.loadError')}: ${data.message}`;
            }
            return;
        }

        currentSkillDir = dirName;
        currentSkillContent = data.content;
        currentSkillModTime = data.modTime;

        editor.value = data.content || '';
        editor.placeholder = '';
    } catch (error) {
        console.error('Error loading skill doc:', error);
        editor.placeholder = `${t('skills.loadError')}: ${error.message}`;
    }
}

// 删除不再需要的函数
// enterEditMode 和 exitEditMode 已不再需要

// 保存文档
async function saveSkillDoc() {
    if (!currentSkillDir) {
        alert(t('skills.noFileSelected'));
        return;
    }

    const editor = document.getElementById('skills-editor');
    if (!editor) return;

    const content = editor.value;
    const relPath = currentSkillDir + '/SKILL.md';

    try {
        const result = await window.go.main.App.WriteSkillFile(
            relPath,
            content,
            currentSkillModTime
        );
        const data = JSON.parse(result);

        if (!data.ok) {
            if (data.code === 'Conflict') {
                const reload = confirm(t('skills.conflictError'));
                if (reload) {
                    await loadSkillDoc(currentSkillDir);
                }
            } else {
                alert(`${t('skills.saveError')}: ${data.message}`);
            }
            return;
        }

        // 更新状态
        currentSkillModTime = data.modTime;
        currentSkillContent = content;

        showSuccessToast(t('skills.saveSuccess'));
    } catch (error) {
        console.error('Error saving skill doc:', error);
        alert(`${t('skills.saveError')}: ${error.message}`);
    }
}

// 显示成功提示
function showSuccessToast(message) {
    const toast = document.createElement('div');
    toast.className = 'toast-success';
    toast.textContent = message;
    toast.style.cssText = `
        position: fixed;
        top: 20px;
        right: 20px;
        background: #4caf50;
        color: white;
        padding: 12px 24px;
        border-radius: 4px;
        box-shadow: 0 2px 8px rgba(0,0,0,0.2);
        z-index: 10000;
        animation: slideIn 0.3s ease-out;
    `;
    document.body.appendChild(toast);

    setTimeout(() => {
        toast.style.animation = 'slideOut 0.3s ease-out';
        setTimeout(() => toast.remove(), 300);
    }, 2000);
}
