import { t } from '../i18n/index.js';
import { formatTokens, maskApiKey, escapeHtml } from '../utils/format.js';
import { getEndpointStats } from './stats.js';
import { toggleEndpoint, testAllEndpointsZeroCost } from './config.js';

const ENDPOINT_TEST_STATUS_KEY = 'ccNexus_endpointTestStatus';
const ENDPOINT_VIEW_MODE_KEY = 'ccNexus_endpointViewMode';
const ENDPOINT_TAB_KEY = 'ccNexus_endpointTab';

// 获取端点测试状态
export function getEndpointTestStatus(endpointID) {
    try {
        const statusMap = JSON.parse(localStorage.getItem(ENDPOINT_TEST_STATUS_KEY) || '{}');
        return statusMap[String(endpointID)]; // true=成功, false=失败, undefined=未测试
    } catch {
        return undefined;
    }
}

// 保存端点测试状态
export function saveEndpointTestStatus(endpointID, success) {
    try {
        const statusMap = JSON.parse(localStorage.getItem(ENDPOINT_TEST_STATUS_KEY) || '{}');
        statusMap[String(endpointID)] = success;
        localStorage.setItem(ENDPOINT_TEST_STATUS_KEY, JSON.stringify(statusMap));
    } catch (error) {
        console.error('Failed to save endpoint test status:', error);
    }
}

// 获取端点视图模式
export function getEndpointViewMode() {
    try {
        return localStorage.getItem(ENDPOINT_VIEW_MODE_KEY) || 'detail';
    } catch {
        return 'detail';
    }
}

// 保存端点视图模式
export function saveEndpointViewMode(mode) {
    try {
        localStorage.setItem(ENDPOINT_VIEW_MODE_KEY, mode);
    } catch (error) {
        console.error('Failed to save endpoint view mode:', error);
    }
}

export function getEndpointTab() {
    try {
        return localStorage.getItem(ENDPOINT_TAB_KEY) || 'claude';
    } catch {
        return 'claude';
    }
}

export function saveEndpointTab(tab) {
    try {
        localStorage.setItem(ENDPOINT_TAB_KEY, tab);
    } catch (error) {
        console.error('Failed to save endpoint tab:', error);
    }
}

export function switchEndpointTab(tab) {
    const normalized = tab === 'codex' ? 'codex' : 'claude';
    saveEndpointTab(normalized);

    const buttons = document.querySelectorAll('.endpoint-tab-btn');
    buttons.forEach(btn => {
        btn.classList.toggle('active', btn.dataset.tab === normalized);
    });

    const contents = document.querySelectorAll('.endpoint-tab-content');
    contents.forEach(content => {
        content.classList.toggle('active', content.dataset.tab === normalized);
    });

    const addButtons = document.querySelectorAll('.endpoint-add-btn');
    addButtons.forEach(btn => {
        btn.classList.toggle('active', btn.dataset.tab === normalized);
    });

}

// 切换视图模式
export function switchEndpointViewMode(mode) {
    saveEndpointViewMode(mode);

    // 更新按钮状态
    const buttons = document.querySelectorAll('.view-mode-btn');
    buttons.forEach(btn => {
        btn.classList.toggle('active', btn.dataset.view === mode);
    });

    // 更新列表样式
    const containers = document.querySelectorAll('.endpoint-list');
    containers.forEach(container => {
        if (mode === 'compact') {
            container.classList.add('compact-view');
        } else {
            container.classList.remove('compact-view');
        }
    });

    // 重新渲染端点列表
    window.loadConfig();
}

// 初始化视图模式
export function initEndpointViewMode() {
    const mode = getEndpointViewMode();
    const buttons = document.querySelectorAll('.view-mode-btn');
    buttons.forEach(btn => {
        btn.classList.toggle('active', btn.dataset.view === mode);
    });
}

export function initEndpointTab() {
    switchEndpointTab(getEndpointTab());
}

let currentTestButton = null;
let currentTestButtonOriginalText = '';
let currentTestID = 0;
let endpointPanelExpanded = true;

function resolvePoolLabel(poolId) {
    return window.codexPoolMap?.[String(poolId)] || `${t('codexPool.pool')} #${poolId}`;
}

function renderEndpointAuthLine(endpoint) {
    const authType = endpoint.authType || 'apikey';
    if (authType === 'codex_pool') {
        const poolLabel = endpoint.codexPoolId ? resolvePoolLabel(endpoint.codexPoolId) : '-';
        return `
            <p style="display: flex; align-items: center; gap: 8px; min-width: 0;">
                <span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">🪪 ${t('modal.authTypeCodexPool')}: ${escapeHtml(poolLabel)}</span>
            </p>
        `;
    }

    return `
        <p style="display: flex; align-items: center; gap: 8px; min-width: 0;">
            <span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">🔑 ${maskApiKey(endpoint.apiKey || '')}</span>
            <button class="copy-btn" data-copy="${endpoint.apiKey || ''}" aria-label="${t('endpoints.copy')}" title="${t('endpoints.copy')}">
                <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" width="1em" height="1em"><path d="M7 4c0-1.1.9-2 2-2h11a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-1V8c0-2-1-3-3-3H7V4Z" fill="currentColor"></path><path d="M5 7a2 2 0 0 0-2 2v10c0 1.1.9 2 2 2h10a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2H5Z" fill="currentColor"></path></svg>
            </button>
        </p>
    `;
}

function copyToClipboard(text, button) {
    navigator.clipboard.writeText(text).then(() => {
        const originalHTML = button.innerHTML;
        button.innerHTML = '<svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" width="1em" height="1em"><path d="M20 6L9 17l-5-5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
        setTimeout(() => { button.innerHTML = originalHTML; }, 1000);
    });
}

export function getTestState() {
    return { currentTestButton, currentTestID };
}

export function clearTestState() {
    if (currentTestButton) {
        currentTestButton.disabled = false;
        currentTestButton.innerHTML = currentTestButtonOriginalText;

        // 恢复简洁视图的 moreBtn
        const endpointItem = currentTestButton.closest('.endpoint-item-compact');
        if (endpointItem) {
            const moreBtn = endpointItem.querySelector('[data-action="more"]');
            if (moreBtn) {
                moreBtn.disabled = false;
                moreBtn.innerHTML = '⋯';
            }
        }

        currentTestButton = null;
        currentTestButtonOriginalText = '';
        currentTestID = 0;
    }
}

export function setTestState(button, id) {
    currentTestButton = button;
    currentTestButtonOriginalText = button.innerHTML;
    currentTestID = id;
}

export async function renderEndpoints(endpoints, clientType, containerId) {
    const container = document.getElementById(containerId);
    if (!container) {
        return;
    }
    container.dataset.clientType = clientType;

    // Get current endpoint from backend
    let currentEndpointName = '';
    try {
        currentEndpointName = await window.go.main.App.GetCurrentEndpoint(clientType);
    } catch (error) {
        console.error('Failed to get current endpoint:', error);
    }

    if (endpoints.length === 0) {
        container.innerHTML = `
            <div class="empty-state">
                <p>${t('endpoints.noEndpoints')}</p>
            </div>
        `;
        return;
    }

    container.innerHTML = '';

    const endpointStats = getEndpointStats();
    // Display endpoints in config file order (no sorting by enabled status)
    const sortedEndpoints = endpoints.map((ep) => {
        const stats = endpointStats[ep.name] || { requests: 0, errors: 0, inputTokens: 0, outputTokens: 0 };
        const enabled = ep.enabled !== undefined ? ep.enabled : true;
        return { endpoint: ep, stats, enabled };
    });

    // 检查视图模式
    const viewMode = getEndpointViewMode();
    if (viewMode === 'compact') {
        container.classList.add('compact-view');
        renderCompactView(sortedEndpoints, container, currentEndpointName, clientType);
        return;
    } else {
        container.classList.remove('compact-view');
    }

    sortedEndpoints.forEach(({ endpoint: ep, stats }) => {
        const totalTokens = stats.inputTokens + stats.outputTokens;
        const enabled = ep.enabled !== undefined ? ep.enabled : true;
        const transformer = ep.transformer || 'claude';
        const model = ep.model || '';
        const endpointID = ep.id || 0;
        const isCurrentEndpoint = ep.name === currentEndpointName;
        const authLine = renderEndpointAuthLine(ep);

        const item = document.createElement('div');
        item.className = 'endpoint-item';
        item.draggable = true;
        item.dataset.name = ep.name;
        item.dataset.id = endpointID;
        // 获取测试状态：true=成功显示✅，false=失败显示❌，undefined/unknown=未测试/未知显示⚠️
        const testStatus = endpointID ? getEndpointTestStatus(endpointID) : undefined;
        let testStatusIcon = '⚠️';
        let testStatusTip = t('endpoints.testTipUnknown');
        if (testStatus === true) {
            testStatusIcon = '✅';
            testStatusTip = t('endpoints.testTipSuccess');
        } else if (testStatus === false) {
            testStatusIcon = '❌';
            testStatusTip = t('endpoints.testTipFailed');
        }

        item.innerHTML = `
            <div class="endpoint-info">
                <h3>
                    <span title="${testStatusTip}" style="cursor: help">${testStatusIcon}</span>
                    ${ep.name}
                    ${!enabled ? '<span class="disabled-badge">' + t('endpoints.disabled') + '</span>' : ''}
                    ${isCurrentEndpoint ? '<span class="current-badge">' + t('endpoints.current') + '</span>' : ''}
                    ${enabled && !isCurrentEndpoint ? '<button class="btn btn-switch" data-action="switch" data-name="' + ep.name + '">' + t('endpoints.switchTo') + '</button>' : ''}
                </h3>
                <p style="display: flex; align-items: center; gap: 8px; min-width: 0;"><span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">🌐 ${ep.apiUrl}</span> <button class="copy-btn" data-copy="${ep.apiUrl}" aria-label="${t('endpoints.copy')}" title="${t('endpoints.copy')}"><svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" width="1em" height="1em"><path d="M7 4c0-1.1.9-2 2-2h11a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-1V8c0-2-1-3-3-3H7V4Z" fill="currentColor"></path><path d="M5 7a2 2 0 0 0-2 2v10c0 1.1.9 2 2 2h10a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2H5Z" fill="currentColor"></path></svg></button></p>
                ${authLine}
                <p style="color: #666; font-size: 14px; margin-top: 5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">🔄 ${t('endpoints.transformer')}: ${transformer}${model ? ` (${model})` : ''}</p>
                <p style="color: #666; font-size: 14px; margin-top: 3px;">📊 ${t('endpoints.requests')}: ${stats.requests} | ${t('endpoints.errors')}: ${stats.errors}</p>
                <p style="color: #666; font-size: 14px; margin-top: 3px;">🎯 ${t('endpoints.tokens')}: ${formatTokens(totalTokens)} (${t('statistics.in')}: ${formatTokens(stats.inputTokens)}, ${t('statistics.out')}: ${formatTokens(stats.outputTokens)})</p>
                ${ep.remark ? `<p style="color: #888; font-size: 13px; margin-top: 5px; font-style: italic;" title="${ep.remark}">💬 ${ep.remark.length > 20 ? ep.remark.substring(0, 20) + '...' : ep.remark}</p>` : ''}
            </div>
            <div class="endpoint-actions">
                <label class="toggle-switch">
                    <input type="checkbox" data-id="${endpointID}" ${enabled ? 'checked' : ''}>
                    <span class="toggle-slider"></span>
                </label>
                <button class="btn-card btn-secondary" data-action="test" data-id="${endpointID}">${t('endpoints.test')}</button>
                <button class="btn-card btn-secondary" data-action="edit" data-id="${endpointID}">${t('endpoints.edit')}</button>
                <button class="btn-card btn-danger" data-action="delete" data-id="${endpointID}">${t('endpoints.delete')}</button>
            </div>
        `;

        const testBtn = item.querySelector('[data-action="test"]');
        const editBtn = item.querySelector('[data-action="edit"]');
        const deleteBtn = item.querySelector('[data-action="delete"]');
        const toggleSwitch = item.querySelector('input[type="checkbox"]');
        const copyBtns = item.querySelectorAll('.copy-btn');

        if (currentTestID !== 0 && currentTestID === endpointID) {
            testBtn.disabled = true;
            testBtn.innerHTML = '⏳';
            currentTestButton = testBtn;
        }

        testBtn.addEventListener('click', () => {
            const id = parseInt(testBtn.getAttribute('data-id'));
            window.testEndpoint(id, testBtn);
        });
        editBtn.addEventListener('click', () => {
            const id = parseInt(editBtn.getAttribute('data-id'));
            window.editEndpoint(id);
        });
        deleteBtn.addEventListener('click', () => {
            const id = parseInt(deleteBtn.getAttribute('data-id'));
            window.deleteEndpoint(id);
        });
        toggleSwitch.addEventListener('change', async (e) => {
            const id = parseInt(e.target.getAttribute('data-id'));
            const newEnabled = e.target.checked;
            try {
                await toggleEndpoint(id, newEnabled);
                window.loadConfig();
            } catch (error) {
                console.error('Failed to toggle endpoint:', error);
                alert('Failed to toggle endpoint: ' + error);
                e.target.checked = !newEnabled;
            }
        });
        copyBtns.forEach(btn => {
            btn.addEventListener('click', () => {
                copyToClipboard(btn.getAttribute('data-copy'), btn);
            });
        });

        // Add switch button event listener
        const switchBtn = item.querySelector('[data-action="switch"]');
        if (switchBtn) {
            switchBtn.addEventListener('click', async () => {
                const name = switchBtn.getAttribute('data-name');
                try {
                    switchBtn.disabled = true;
                    switchBtn.innerHTML = '⏳';
                    await window.go.main.App.SwitchToEndpoint(name, clientType);
                    window.loadConfig(); // Refresh display
                } catch (error) {
                    console.error('Failed to switch endpoint:', error);
                    alert(t('endpoints.switchFailed') + ': ' + error);
                } finally {
                    if (switchBtn) {
                        switchBtn.disabled = false;
                        switchBtn.innerHTML = t('endpoints.switchTo');
                    }
                }
            });
        }

        // Add drag and drop event listeners
        setupDragAndDrop(item, container, clientType);

        container.appendChild(item);
    });
}

export function toggleEndpointPanel() {
    const panel = document.getElementById('endpointPanel');
    const icon = document.getElementById('endpointToggleIcon');
    const text = document.getElementById('endpointToggleText');

    endpointPanelExpanded = !endpointPanelExpanded;

    if (endpointPanelExpanded) {
        panel.style.display = 'block';
        icon.textContent = '🔼';
        text.textContent = t('endpoints.collapse');
    } else {
        panel.style.display = 'none';
        icon.textContent = '🔽';
        text.textContent = t('endpoints.expand');
    }
}

// Drag and drop state
let draggedElement = null;
let draggedOverElement = null;
let draggedOriginalName = null;
let autoScrollInterval = null;

// Auto scroll when dragging near edges
function autoScroll(e) {
    const scrollContainer = document.querySelector('.container');
    const scrollThreshold = 80;
    const scrollSpeed = 10;

    const rect = scrollContainer.getBoundingClientRect();
    const distanceFromTop = e.clientY - rect.top;
    const distanceFromBottom = rect.bottom - e.clientY;

    if (distanceFromTop < scrollThreshold) {
        scrollContainer.scrollTop -= scrollSpeed;
    } else if (distanceFromBottom < scrollThreshold) {
        scrollContainer.scrollTop += scrollSpeed;
    }
}

// Setup drag and drop for an endpoint item
function setupDragAndDrop(item, container, clientType) {
    item.addEventListener('dragstart', (e) => {
        draggedElement = item;
        draggedOriginalName = item.dataset.name;
        item.classList.add('dragging');
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/html', item.innerHTML);

        // Start auto-scroll interval
        autoScrollInterval = setInterval(() => {
            if (window.lastDragEvent) {
                autoScroll(window.lastDragEvent);
            }
        }, 50);
    });

    item.addEventListener('dragend', (e) => {
        item.classList.remove('dragging');
        const allItems = container.querySelectorAll('.endpoint-item');
        allItems.forEach(i => i.classList.remove('drag-over'));
        draggedElement = null;
        draggedOverElement = null;
        draggedOriginalName = null;

        // Clear auto-scroll
        if (autoScrollInterval) {
            clearInterval(autoScrollInterval);
            autoScrollInterval = null;
        }
        window.lastDragEvent = null;
    });

    item.addEventListener('dragover', (e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        window.lastDragEvent = e; // Store for auto-scroll

        if (draggedElement && draggedElement !== item) {
            if (draggedOverElement && draggedOverElement !== item) {
                draggedOverElement.classList.remove('drag-over');
            }
            item.classList.add('drag-over');
            draggedOverElement = item;
        }
    });

    item.addEventListener('dragleave', (e) => {
        // Only remove if we're actually leaving the element
        if (!item.contains(e.relatedTarget)) {
            item.classList.remove('drag-over');
            if (draggedOverElement === item) {
                draggedOverElement = null;
            }
        }
    });

    item.addEventListener('drop', async (e) => {
        e.preventDefault();
        e.stopPropagation();

        if (draggedElement && draggedElement !== item) {
            // Use dataset.name to identify positions, not DOM order
            const draggedName = draggedElement.dataset.name;
            const targetName = item.dataset.name;

            // Get all items and build current order by name
            const allItems = Array.from(container.querySelectorAll('.endpoint-item'));
            const currentOrder = allItems.map(el => el.dataset.name);

            // Find positions by name (stable, not affected by scrolling)
            const fromIndex = currentOrder.indexOf(draggedName);
            const toIndex = currentOrder.indexOf(targetName);

            // Calculate new order
            const newOrder = [...currentOrder];
            newOrder.splice(fromIndex, 1);
            newOrder.splice(toIndex, 0, draggedName);

            // Compare arrays: if order hasn't changed, don't do anything
            const orderChanged = !currentOrder.every((name, idx) => name === newOrder[idx]);

            if (!orderChanged) {
                item.classList.remove('drag-over');
                return;
            }

            // Save to backend
            try {
                await window.go.main.App.ReorderEndpoints(newOrder, clientType);
                window.loadConfig();
            } catch (error) {
                console.error('Failed to reorder endpoints:', error);
                alert(t('endpoints.reorderFailed') + ': ' + error);
                window.loadConfig();
            }
        }

        item.classList.remove('drag-over');
    });
}

// 初始化端点成功事件监听
export function initEndpointSuccessListener() {
    if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn('endpoint:success', (endpointName) => {
            let endpointID = 0;
            if (window.latestConfig?.endpoints) {
                const endpoint = window.latestConfig.endpoints.find(ep => ep.name === endpointName);
                endpointID = endpoint?.id || 0;
            }
            if (endpointID) {
                // 更新测试状态为成功
                saveEndpointTestStatus(endpointID, true);
            }
            // 刷新端点列表显示
            if (window.loadConfig) {
                window.loadConfig();
            }
        });
    }
}

// 清除所有端点测试状态
export function clearAllEndpointTestStatus() {
    try {
        localStorage.removeItem(ENDPOINT_TEST_STATUS_KEY);
    } catch (error) {
        console.error('Failed to clear endpoint test status:', error);
    }
}

// 启动时零消耗检测所有端点
export async function checkAllEndpointsOnStartup() {
    try {
        // 先清除所有状态
        clearAllEndpointTestStatus();

        const results = await testAllEndpointsZeroCost();
        let nameToID = {};
        if (window.latestConfig?.endpoints) {
            nameToID = Object.fromEntries(window.latestConfig.endpoints.map(ep => [ep.name, ep.id]));
        } else {
            try {
                const configStr = await window.go.main.App.GetConfig();
                const config = JSON.parse(configStr);
                nameToID = Object.fromEntries(config.endpoints.map(ep => [ep.name, ep.id]));
            } catch (error) {
                console.error('Failed to map endpoint ids:', error);
            }
        }
        for (const [name, status] of Object.entries(results)) {
            const endpointID = nameToID[name];
            if (!endpointID) {
                continue;
            }
            if (status === 'ok') {
                saveEndpointTestStatus(endpointID, true);
            } else if (status === 'invalid_key') {
                saveEndpointTestStatus(endpointID, false);
            }
            // 'unknown' 保持未设置状态，显示 ⚠️
        }
        // 刷新端点列表显示
        if (window.loadConfig) {
            window.loadConfig();
        }
    } catch (error) {
        console.error('Failed to check endpoints on startup:', error);
    }
}

// 渲染简洁视图
function renderCompactView(sortedEndpoints, container, currentEndpointName, clientType) {
    sortedEndpoints.forEach(({ endpoint: ep, stats }) => {
        const enabled = ep.enabled !== undefined ? ep.enabled : true;
        const transformer = ep.transformer || 'claude';
        const model = ep.model || '';
        const endpointID = ep.id || 0;
        const isCurrentEndpoint = ep.name === currentEndpointName;

        // 获取测试状态
        const testStatus = endpointID ? getEndpointTestStatus(endpointID) : undefined;
        let testStatusIcon = '⚠️';
        let testStatusTip = t('endpoints.testTipUnknown');
        if (testStatus === true) {
            testStatusIcon = '✅';
            testStatusTip = t('endpoints.testTipSuccess');
        } else if (testStatus === false) {
            testStatusIcon = '❌';
            testStatusTip = t('endpoints.testTipFailed');
        }

        const item = document.createElement('div');
        item.className = 'endpoint-item-compact';
        item.draggable = true;
        item.dataset.name = ep.name;
        item.dataset.id = endpointID;

        // 截断 URL 显示
        const displayUrl = ep.apiUrl.length > 40 ? ep.apiUrl.substring(0, 40) + '...' : ep.apiUrl;
        const authType = ep.authType || 'apikey';
        const authLabel = authType === 'codex_pool'
            ? `${t('modal.authTypeCodexPool')}: ${resolvePoolLabel(ep.codexPoolId)}`
            : `${t('modal.apiKey')}: ${maskApiKey(ep.apiKey || '')}`;

        // 构建统计详情提示
        const totalTokens = stats.inputTokens + stats.outputTokens;
        let statsTooltip = `${t('endpoints.requests')}: ${stats.requests} | ${t('endpoints.errors')}: ${stats.errors}\n${t('statistics.in')}: ${formatTokens(stats.inputTokens)} | ${t('statistics.out')}: ${formatTokens(stats.outputTokens)}\n${authLabel}`;
        if (model) {
            statsTooltip += `\n${t('modal.model')}: ${model}`;
        }
        if (ep.remark) {
            statsTooltip += `\n${t('modal.remark')}: ${ep.remark}`;
        }

        item.innerHTML = `
            <div class="drag-handle" title="${t('endpoints.dragToReorder')}">
                <div class="drag-handle-dots"><span></span><span></span></div>
                <div class="drag-handle-dots"><span></span><span></span></div>
                <div class="drag-handle-dots"><span></span><span></span></div>
            </div>
            <span class="compact-status" title="${testStatusTip}" style="cursor: help">${testStatusIcon}</span>
            <span class="compact-name" title="${ep.name}">${ep.name}</span>
            ${isCurrentEndpoint ? '<span class="btn btn-primary compact-badge-btn">' + t('endpoints.current') + '</span>' : (enabled ? '<button class="btn btn-primary compact-badge-btn" data-action="switch" data-name="' + ep.name + '">' + t('endpoints.switchTo') + '</button>' : '<span class="btn btn-primary compact-badge-btn compact-badge-disabled">' + t('endpoints.disabled') + '</span>')}
            <span class="compact-url" title="${ep.apiUrl}"><span class="compact-url-icon">🌐</span>${displayUrl}</span>
            <span class="compact-transformer">🔄 ${transformer}</span>
            <span class="compact-stats" title="${statsTooltip}">📊 ${stats.requests} | 🎯 ${formatTokens(stats.inputTokens + stats.outputTokens)}</span>
            <div class="compact-actions">
                <label class="toggle-switch">
                    <input type="checkbox" data-id="${endpointID}" ${enabled ? 'checked' : ''}>
                    <span class="toggle-slider"></span>
                </label>
                <div class="compact-more-dropdown">
                    <button class="compact-btn" data-action="more" title="${t('endpoints.moreActions')}">⋯</button>
                    <div class="compact-more-menu">
                        <button data-action="test" data-id="${endpointID}">🧪 ${t('endpoints.test')}</button>
                        <button data-action="edit" data-id="${endpointID}">✏️ ${t('endpoints.edit')}</button>
                        <button data-action="delete" data-id="${endpointID}" class="danger">🗑️ ${t('endpoints.delete')}</button>
                    </div>
                </div>
            </div>
        `;

        // 绑定事件
        bindCompactItemEvents(item, endpointID, enabled, clientType);

        // 设置拖拽
        setupCompactDragAndDrop(item, container);

        container.appendChild(item);
    });

    // 点击其他地方关闭下拉菜单（先移除旧监听器，避免重复绑定）
    document.removeEventListener('click', closeAllDropdowns);
    document.addEventListener('click', closeAllDropdowns);
}

// 绑定简洁视图项目事件
function bindCompactItemEvents(item, endpointID, enabled, clientType) {
    const toggleSwitch = item.querySelector('input[type="checkbox"]');
    const switchBtn = item.querySelector('[data-action="switch"]');
    const moreBtn = item.querySelector('[data-action="more"]');
    const moreMenu = item.querySelector('.compact-more-menu');
    const testBtn = item.querySelector('[data-action="test"]');
    const editBtn = item.querySelector('[data-action="edit"]');
    const deleteBtn = item.querySelector('[data-action="delete"]');

    // 如果当前正在测试这个端点，显示加载状态
    if (currentTestID !== 0 && currentTestID === endpointID) {
        moreBtn.innerHTML = '⏳';
        moreBtn.disabled = true;
        currentTestButton = testBtn;
    }

    // 启用/禁用开关
    toggleSwitch.addEventListener('change', async (e) => {
        const id = parseInt(e.target.getAttribute('data-id'));
        const newEnabled = e.target.checked;
        try {
            await toggleEndpoint(id, newEnabled);
            window.loadConfig();
        } catch (error) {
            console.error('Failed to toggle endpoint:', error);
            alert('Failed to toggle endpoint: ' + error);
            e.target.checked = !newEnabled;
        }
    });

    // 切换按钮
    if (switchBtn) {
        switchBtn.addEventListener('click', async () => {
            const name = switchBtn.getAttribute('data-name');
            try {
                switchBtn.disabled = true;
                switchBtn.innerHTML = '⏳';
            await window.go.main.App.SwitchToEndpoint(name, clientType);
            window.loadConfig(); // Refresh display
            } catch (error) {
                console.error('Failed to switch endpoint:', error);
                alert(t('endpoints.switchFailed') + ': ' + error);
            } finally {
                if (switchBtn) {
                    switchBtn.disabled = false;
                    switchBtn.innerHTML = t('endpoints.switchTo');
                }
            }
        });
    }

    // 更多操作按钮
    moreBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        const isOpen = moreMenu.classList.contains('show');
        closeAllDropdowns();
        if (!isOpen) {
            moreMenu.classList.add('show');
        }
    });

    // 测试按钮
    testBtn.addEventListener('click', () => {
        closeAllDropdowns();
        const id = parseInt(testBtn.getAttribute('data-id'));
        window.testEndpoint(id, testBtn);
    });

    // 编辑按钮
    editBtn.addEventListener('click', () => {
        closeAllDropdowns();
        const id = parseInt(editBtn.getAttribute('data-id'));
        window.editEndpoint(id);
    });

    // 删除按钮
    deleteBtn.addEventListener('click', () => {
        closeAllDropdowns();
        const id = parseInt(deleteBtn.getAttribute('data-id'));
        window.deleteEndpoint(id);
    });
}

// 关闭所有下拉菜单
function closeAllDropdowns() {
    document.querySelectorAll('.compact-more-menu.show').forEach(menu => {
        menu.classList.remove('show');
    });
}

// 检查是否有下拉菜单正在显示
export function isDropdownOpen() {
    return document.querySelectorAll('.compact-more-menu.show').length > 0;
}

// 拖拽占位符元素
let dragPlaceholder = null;
let draggedItemHeight = 0;

// 创建占位符（指示线）
function createPlaceholder() {
    const placeholder = document.createElement('div');
    placeholder.className = 'drag-placeholder';
    return placeholder;
}

// 更新其他元素的位置
function updateItemPositions(container, draggedElement, placeholder) {
    const allItems = Array.from(container.querySelectorAll('.endpoint-item-compact'));
    const draggedIndex = allItems.indexOf(draggedElement);

    // 计算占位符在端点元素中的目标索引
    let targetIndex = 0;
    let currentNode = placeholder.previousSibling;
    while (currentNode) {
        if (currentNode.classList && currentNode.classList.contains('endpoint-item-compact')) {
            targetIndex++;
        }
        currentNode = currentNode.previousSibling;
    }

    allItems.forEach((item, index) => {
        let offset = 0;

        if (item === draggedElement) {
            // 被拖拽元素视觉上移动到占位符位置
            offset = (targetIndex - draggedIndex) * (draggedItemHeight + 8);
        } else if (draggedIndex < targetIndex) {
            // 向下拖拽：draggedIndex 和 targetIndex 之间的元素向上移
            if (index > draggedIndex && index < targetIndex) {
                offset = -(draggedItemHeight + 8);
            }
        } else if (draggedIndex > targetIndex) {
            // 向上拖拽：targetIndex 和 draggedIndex 之间的元素向下移
            if (index >= targetIndex && index < draggedIndex) {
                offset = draggedItemHeight + 8;
            }
        }

        item.style.transform = offset !== 0 ? `translateY(${offset}px)` : '';
    });
}

// 根据鼠标位置移动占位符
function movePlaceholderByMousePosition(e, container, draggedElement, dragPlaceholder) {
    if (!draggedElement || !dragPlaceholder) return;

    const allItems = Array.from(container.querySelectorAll('.endpoint-item-compact'));
    const mouseY = e.clientY;

    // 找到最接近鼠标位置的元素
    let closestItem = null;
    let closestDistance = Infinity;
    let insertBefore = true;

    allItems.forEach(item => {
        if (item === draggedElement) return;

        const rect = item.getBoundingClientRect();
        const itemMiddle = rect.top + rect.height / 2;
        const distance = Math.abs(mouseY - itemMiddle);

        if (distance < closestDistance) {
            closestDistance = distance;
            closestItem = item;
            insertBefore = mouseY < itemMiddle;
        }
    });

    // 移动占位符
    if (closestItem) {
        const targetPosition = insertBefore ? closestItem : closestItem.nextSibling;
        if (targetPosition !== dragPlaceholder && targetPosition !== dragPlaceholder.nextSibling) {
            container.insertBefore(dragPlaceholder, targetPosition);
            updateItemPositions(container, draggedElement, dragPlaceholder);
        }
    } else if (allItems.length === 1) {
        // 只有一个元素（被拖拽的元素）
        if (dragPlaceholder.parentNode !== container) {
            container.appendChild(dragPlaceholder);
        }
    }
}

// 简洁视图的拖拽设置
function setupCompactDragAndDrop(item, container) {
    item.addEventListener('dragstart', (e) => {
        draggedElement = item;
        draggedOriginalName = item.dataset.name;
        draggedItemHeight = item.offsetHeight;
        item.classList.add('dragging');
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/html', item.innerHTML);

        // 创建并插入占位符（指示线）
        dragPlaceholder = createPlaceholder();
        item.parentNode.insertBefore(dragPlaceholder, item.nextSibling);

        // 在容器上添加事件监听
        container.addEventListener('dragover', handleContainerDragOver);
        container.addEventListener('drop', handleContainerDrop);

        autoScrollInterval = setInterval(() => {
            if (window.lastDragEvent) {
                autoScroll(window.lastDragEvent);
            }
        }, 50);
    });

    item.addEventListener('dragend', () => {
        item.classList.remove('dragging');
        const allItems = container.querySelectorAll('.endpoint-item-compact');
        allItems.forEach(i => {
            i.classList.remove('drag-over');
            i.style.transform = '';
        });

        // 清理容器的 cursor 样式
        container.style.cursor = '';

        // 移除容器的事件监听
        container.removeEventListener('dragover', handleContainerDragOver);
        container.removeEventListener('drop', handleContainerDrop);

        // 移除占位符
        if (dragPlaceholder && dragPlaceholder.parentNode) {
            dragPlaceholder.parentNode.removeChild(dragPlaceholder);
            dragPlaceholder = null;
        }

        draggedElement = null;
        draggedOverElement = null;
        draggedOriginalName = null;
        draggedItemHeight = 0;

        if (autoScrollInterval) {
            clearInterval(autoScrollInterval);
            autoScrollInterval = null;
        }
        window.lastDragEvent = null;
    });

    // 在端点元素上禁止 drop（但允许事件冒泡到容器，让占位符能正常移动）
    item.addEventListener('dragover', (e) => {
        e.preventDefault();
        // 移除 stopPropagation()，让事件冒泡到容器
        e.dataTransfer.dropEffect = 'none';
    });
}

// 容器的 dragover 处理函数
function handleContainerDragOver(e) {
    e.preventDefault();
    window.lastDragEvent = e;

    const container = e.currentTarget;

    // 检查鼠标是否在端点元素上
    const isOverEndpointItem = e.target.closest('.endpoint-item-compact');

    if (isOverEndpointItem) {
        // 在端点元素上：显示禁止图标，但仍然移动占位符
        e.dataTransfer.dropEffect = 'none';
        container.style.cursor = 'no-drop';
    } else {
        // 在空白区域或占位符上：显示允许图标
        e.dataTransfer.dropEffect = 'move';
        container.style.cursor = 'grabbing';
    }

    // 始终更新占位符位置，让其他元素自动移开
    movePlaceholderByMousePosition(e, container, draggedElement, dragPlaceholder);
}

// 容器的 drop 处理函数
async function handleContainerDrop(e) {
    if (e.target.closest('.endpoint-item-compact')) {
        return;
    }
    e.preventDefault();
    e.stopPropagation();

    const container = e.currentTarget;
    if (draggedElement && dragPlaceholder) {
        const draggedName = draggedElement.dataset.name;
        const allItems = Array.from(container.querySelectorAll('.endpoint-item-compact'));
        const currentOrder = allItems.map(el => el.dataset.name);
        const allChildren = Array.from(container.children);
        const placeholderIndex = allChildren.indexOf(dragPlaceholder);

        let targetIndex = 0;
        for (let i = 0; i < placeholderIndex; i++) {
            if (allChildren[i].classList.contains('endpoint-item-compact')) {
                targetIndex++;
            }
        }

        const draggedIndex = currentOrder.indexOf(draggedName);
        if (draggedIndex < targetIndex) {
            targetIndex--;
        }

        const newOrder = [...currentOrder];
        newOrder.splice(draggedIndex, 1);
        newOrder.splice(targetIndex, 0, draggedName);

        const orderChanged = !currentOrder.every((name, idx) => name === newOrder[idx]);
        if (!orderChanged) return;

        const clientType = container.dataset.clientType || 'claude';
        try {
            await window.go.main.App.ReorderEndpoints(newOrder, clientType);
            window.loadConfig();
        } catch (error) {
            console.error('Failed to reorder endpoints:', error);
            alert(t('endpoints.reorderFailed') + ': ' + error);
            window.loadConfig();
        }
    }
}
