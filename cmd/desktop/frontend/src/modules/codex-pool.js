import { t } from '../i18n/index.js';
import { escapeHtml } from '../utils/format.js';

let codexSlots = [];
let codexPools = [];
let currentSlotEditId = 0;
let currentPoolEditId = 0;

function parseJSONResult(resultStr, key) {
    const result = JSON.parse(resultStr || '{}');
    if (!result.success) {
        throw new Error(result.message || 'request failed');
    }
    return key ? (result[key] ?? null) : result;
}

async function listCodexSlots() {
    return parseJSONResult(await window.go.main.App.ListCodexSlots(), 'slots') || [];
}

async function listCodexPools() {
    return parseJSONResult(await window.go.main.App.ListCodexPools(), 'pools') || [];
}

function updateGlobalCache() {
    window.codexSlotsCache = codexSlots;
    window.codexPoolsCache = codexPools;
    window.codexPoolMap = Object.fromEntries(codexPools.map(pool => [String(pool.id), pool.name]));
}

function getPoolName(poolId) {
    return window.codexPoolMap?.[String(poolId)] || `${t('codexPool.pool')} #${poolId}`;
}

function getSlotName(slotId) {
    const slot = codexSlots.find(item => item.id === slotId);
    return slot?.name || `#${slotId}`;
}

function statusText(status) {
    return t(`codexPool.statusValues.${status || 'unknown'}`);
}

function statusClass(status) {
    return `codex-status-${status || 'unknown'}`;
}

function formatDateTime(value) {
    if (!value) {
        return '-';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return '-';
    }
    return date.toLocaleString();
}

function formatRemainingTime(value) {
    if (!value) {
        return '-';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
        return '-';
    }
    const diffMs = date.getTime() - Date.now();
    if (diffMs <= 0) {
        return '-';
    }
    const minutes = Math.ceil(diffMs / 60000);
    return `${minutes}${t('codexPool.minutes')}`;
}

function truncatePath(path) {
    if (!path) {
        return '-';
    }
    if (path.length <= 42) {
        return path;
    }
    return `${path.slice(0, 18)}...${path.slice(-18)}`;
}

function getSlotNote(slot) {
    if (slot.status === 'cooling' && slot.cooldownUntil) {
        return t('codexPool.cooldownUntil').replace('{time}', formatDateTime(slot.cooldownUntil));
    }
    if (slot.lastError) {
        return slot.lastError;
    }
    if (slot.remark) {
        return slot.remark;
    }
    return t('codexPool.noExtraInfo');
}

function getPoolNote(pool) {
    const slotNames = (pool.slotIds || []).map(getSlotName).join(', ');
    if (slotNames) {
        return `${t('codexPool.boundSlots')}: ${slotNames}`;
    }
    return t('codexPool.noSlots');
}

function getSelectedSlotIds() {
    return Array.from(document.querySelectorAll('#codexPoolSlotList input[type="checkbox"]:checked'))
        .map(input => parseInt(input.value, 10))
        .filter(id => Number.isInteger(id) && id > 0);
}

function renderPoolSlotCheckboxes(selectedIds = []) {
    const container = document.getElementById('codexPoolSlotList');
    if (!container) {
        return;
    }

    if (codexSlots.length === 0) {
        container.innerHTML = `<div class="codex-empty-tip">${t('codexPool.noSlotsForPool')}</div>`;
        return;
    }

    const selectedSet = new Set(selectedIds);
    container.innerHTML = codexSlots.map((slot) => `
        <label class="codex-checkbox-item">
            <input type="checkbox" value="${slot.id}" ${selectedSet.has(slot.id) ? 'checked' : ''}>
            <span class="codex-checkbox-main">
                <span class="codex-checkbox-title">${escapeHtml(slot.name)}</span>
                <span class="codex-checkbox-meta">${statusText(slot.status)}${slot.enabled ? '' : ` · ${t('codexPool.disabled')}`}</span>
            </span>
        </label>
    `).join('');
}

async function confirmAction(message) {
    if (typeof window.showConfirm === 'function') {
        return window.showConfirm(message);
    }
    return window.confirm(message);
}

function notify(message, type = 'info') {
    if (typeof window.showNotification === 'function') {
        window.showNotification(message, type);
        return;
    }
    if (type === 'error') {
        window.alert(message);
    }
}

async function refreshDashboard() {
    if (typeof window.loadConfig === 'function') {
        await window.loadConfig();
        return;
    }
    await loadCodexPoolData({ silent: true });
}

export async function loadCodexPoolData(options = {}) {
    const { silent = false } = options;

    try {
        const [slots, pools] = await Promise.all([listCodexSlots(), listCodexPools()]);
        codexSlots = Array.isArray(slots) ? slots : [];
        codexPools = Array.isArray(pools) ? pools : [];
        updateGlobalCache();
        renderCodexSlots();
        renderCodexPools();
    } catch (error) {
        console.error('Failed to load Codex pool data:', error);
        if (!silent) {
            notify(t('codexPool.loadFailed').replace('{error}', error.message || error), 'error');
        }
    }
}

export function renderCodexSlots() {
    const container = document.getElementById('codexSlotList');
    if (!container) {
        return;
    }

    if (codexSlots.length === 0) {
        container.innerHTML = `<div class="empty-state"><p>${t('codexPool.noSlots')}</p></div>`;
        return;
    }

    container.innerHTML = codexSlots.map((slot) => `
        <div class="codex-resource-item">
            <div class="codex-resource-main">
                <div class="codex-resource-title">
                    <div class="codex-resource-heading">
                        <h3>${escapeHtml(slot.name)}</h3>
                        ${slot.enabled ? '' : `<span class="codex-inline-badge codex-inline-badge-muted">${t('codexPool.disabled')}</span>`}
                    </div>
                    <span class="codex-status-badge ${statusClass(slot.status)}">${statusText(slot.status)}</span>
                </div>
                <div class="codex-resource-meta-grid">
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.accountId')}</span>
                        <span class="codex-meta-value">${escapeHtml(slot.accountId || '-')}</span>
                    </div>
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.stateDir')}</span>
                        <span class="codex-meta-value" title="${escapeHtml(slot.stateDir || '')}">${escapeHtml(truncatePath(slot.stateDir))}</span>
                    </div>
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.cooldown')}</span>
                        <span class="codex-meta-value">${escapeHtml(formatRemainingTime(slot.cooldownUntil))}</span>
                    </div>
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.lastUsedAt')}</span>
                        <span class="codex-meta-value">${escapeHtml(formatDateTime(slot.lastUsedAt))}</span>
                    </div>
                </div>
                <div class="codex-resource-note" title="${escapeHtml(getSlotNote(slot))}">${escapeHtml(getSlotNote(slot))}</div>
            </div>
            <div class="codex-resource-actions">
                <button class="btn-card btn-secondary" onclick="window.launchCodexSlotLogin(${slot.id})">${t('codexPool.login')}</button>
                <button class="btn-card btn-secondary" onclick="window.syncCodexSlotStatus(${slot.id})">${t('codexPool.sync')}</button>
                <button class="btn-card btn-secondary" onclick="window.showEditCodexSlotModal(${slot.id})">${t('endpoints.edit')}</button>
                <button class="btn-card btn-danger" onclick="window.deleteCodexSlot(${slot.id})">${t('endpoints.delete')}</button>
            </div>
        </div>
    `).join('');
}

export function renderCodexPools() {
    const container = document.getElementById('codexPoolList');
    if (!container) {
        return;
    }

    if (codexPools.length === 0) {
        container.innerHTML = `<div class="empty-state"><p>${t('codexPool.noPools')}</p></div>`;
        return;
    }

    container.innerHTML = codexPools.map((pool) => `
        <div class="codex-resource-item">
            <div class="codex-resource-main">
                <div class="codex-resource-title">
                    <div class="codex-resource-heading">
                        <h3>${escapeHtml(pool.name)}</h3>
                        ${pool.enabled ? '' : `<span class="codex-inline-badge codex-inline-badge-muted">${t('codexPool.disabled')}</span>`}
                    </div>
                    <span class="codex-status-badge ${pool.enabled ? 'codex-status-ready' : 'codex-status-disabled'}">${pool.enabled ? t('codexPool.enabled') : t('codexPool.disabled')}</span>
                </div>
                <div class="codex-resource-meta-grid">
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.slotCount')}</span>
                        <span class="codex-meta-value">${(pool.slotIds || []).length}</span>
                    </div>
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.strategy')}</span>
                        <span class="codex-meta-value">${escapeHtml(pool.strategy || 'rr')}</span>
                    </div>
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.cooldown429Sec')}</span>
                        <span class="codex-meta-value">${pool.cooldown429Sec || 0}s</span>
                    </div>
                    <div class="codex-meta-item">
                        <span class="codex-meta-label">${t('codexPool.cooldown5xxSec')}</span>
                        <span class="codex-meta-value">${pool.cooldown5xxSec || 0}s</span>
                    </div>
                </div>
                <div class="codex-resource-note" title="${escapeHtml(getPoolNote(pool))}">
                    ${escapeHtml(`${t('codexPool.authExpiredPolicy')}: ${pool.authExpiredPolicy || 'skip'} · ${getPoolNote(pool)}`)}
                </div>
            </div>
            <div class="codex-resource-actions">
                <button class="btn-card btn-secondary" onclick="window.showEditCodexPoolModal(${pool.id})">${t('endpoints.edit')}</button>
                <button class="btn-card btn-danger" onclick="window.deleteCodexPool(${pool.id})">${t('endpoints.delete')}</button>
            </div>
        </div>
    `).join('');
}

export function showAddCodexSlotModal() {
    currentSlotEditId = 0;
    document.getElementById('codexSlotModalTitle').textContent = t('codexPool.addSlot');
    document.getElementById('codexSlotName').value = '';
    document.getElementById('codexSlotAccountId').value = '';
    document.getElementById('codexSlotRemark').value = '';
    document.getElementById('codexSlotEnabled').checked = true;
    document.getElementById('codexSlotModal').classList.add('active');
}

export function showEditCodexSlotModal(id) {
    const slot = codexSlots.find(item => item.id === id);
    if (!slot) {
        notify(t('codexPool.slotNotFound'), 'error');
        return;
    }

    currentSlotEditId = id;
    document.getElementById('codexSlotModalTitle').textContent = t('codexPool.editSlot');
    document.getElementById('codexSlotName').value = slot.name || '';
    document.getElementById('codexSlotAccountId').value = slot.accountId || '';
    document.getElementById('codexSlotRemark').value = slot.remark || '';
    document.getElementById('codexSlotEnabled').checked = slot.enabled !== false;
    document.getElementById('codexSlotModal').classList.add('active');
}

export function closeCodexSlotModal() {
    document.getElementById('codexSlotModal').classList.remove('active');
}

export async function saveCodexSlot() {
    const name = document.getElementById('codexSlotName').value.trim();
    const accountId = document.getElementById('codexSlotAccountId').value.trim();
    const remark = document.getElementById('codexSlotRemark').value.trim();
    const enabled = document.getElementById('codexSlotEnabled').checked;

    if (!name) {
        notify(t('codexPool.slotNameRequired'), 'error');
        return;
    }

    try {
        let slotId = currentSlotEditId;
        if (slotId === 0) {
            const createdSlot = parseJSONResult(await window.go.main.App.CreateCodexSlot(name, accountId, remark), 'slot');
            slotId = createdSlot?.id || 0;
        }

        parseJSONResult(await window.go.main.App.UpdateCodexSlot(slotId, name, accountId, remark, enabled), 'slot');
        closeCodexSlotModal();
        notify(t('codexPool.slotSaved'), 'success');
        await refreshDashboard();
    } catch (error) {
        console.error('Failed to save Codex slot:', error);
        notify(t('codexPool.saveFailed').replace('{error}', error.message || error), 'error');
    }
}

export async function syncCodexSlotStatus(id) {
    try {
        parseJSONResult(await window.go.main.App.SyncCodexSlotStatus(id), 'slot');
        notify(t('codexPool.slotSynced'), 'success');
        await refreshDashboard();
    } catch (error) {
        console.error('Failed to sync Codex slot status:', error);
        notify(t('codexPool.syncFailed').replace('{error}', error.message || error), 'error');
    }
}

export async function launchCodexSlotLogin(id) {
    try {
        await window.go.main.App.LaunchCodexSlotLogin(id);
        notify(t('codexPool.loginLaunched'), 'success');
        let attempts = 0;
        const timer = setInterval(() => {
            attempts += 1;
            loadCodexPoolData({ silent: true });
            if (attempts >= 20) {
                clearInterval(timer);
            }
        }, 3000);
    } catch (error) {
        console.error('Failed to launch Codex slot login:', error);
        notify(t('codexPool.loginLaunchFailed').replace('{error}', error.message || error), 'error');
    }
}

export async function deleteCodexSlot(id) {
    const slot = codexSlots.find(item => item.id === id);
    if (!slot) {
        notify(t('codexPool.slotNotFound'), 'error');
        return;
    }

    const confirmed = await confirmAction(t('codexPool.confirmDeleteSlot').replace('{name}', slot.name));
    if (!confirmed) {
        return;
    }

    try {
        await window.go.main.App.DeleteCodexSlot(id);
        notify(t('codexPool.slotDeleted'), 'success');
        await refreshDashboard();
    } catch (error) {
        console.error('Failed to delete Codex slot:', error);
        notify(t('codexPool.deleteFailed').replace('{error}', error.message || error), 'error');
    }
}

export function showAddCodexPoolModal() {
    currentPoolEditId = 0;
    document.getElementById('codexPoolModalTitle').textContent = t('codexPool.addPool');
    document.getElementById('codexPoolName').value = '';
    document.getElementById('codexPoolStrategy').value = 'rr';
    document.getElementById('codexPoolEnabled').checked = true;
    document.getElementById('codexPoolCooldown429').value = '600';
    document.getElementById('codexPoolCooldown5xx').value = '120';
    document.getElementById('codexPoolAuthExpiredPolicy').value = 'skip';
    renderPoolSlotCheckboxes([]);
    document.getElementById('codexPoolModal').classList.add('active');
}

export function showEditCodexPoolModal(id) {
    const pool = codexPools.find(item => item.id === id);
    if (!pool) {
        notify(t('codexPool.poolNotFound'), 'error');
        return;
    }

    currentPoolEditId = id;
    document.getElementById('codexPoolModalTitle').textContent = t('codexPool.editPool');
    document.getElementById('codexPoolName').value = pool.name || '';
    document.getElementById('codexPoolStrategy').value = pool.strategy || 'rr';
    document.getElementById('codexPoolEnabled').checked = pool.enabled !== false;
    document.getElementById('codexPoolCooldown429').value = String(pool.cooldown429Sec || 600);
    document.getElementById('codexPoolCooldown5xx').value = String(pool.cooldown5xxSec || 120);
    document.getElementById('codexPoolAuthExpiredPolicy').value = pool.authExpiredPolicy || 'skip';
    renderPoolSlotCheckboxes(pool.slotIds || []);
    document.getElementById('codexPoolModal').classList.add('active');
}

export function closeCodexPoolModal() {
    document.getElementById('codexPoolModal').classList.remove('active');
}

export async function saveCodexPool() {
    const name = document.getElementById('codexPoolName').value.trim();
    const strategy = document.getElementById('codexPoolStrategy').value || 'rr';
    const enabled = document.getElementById('codexPoolEnabled').checked;
    const cooldown429Sec = parseInt(document.getElementById('codexPoolCooldown429').value, 10) || 600;
    const cooldown5xxSec = parseInt(document.getElementById('codexPoolCooldown5xx').value, 10) || 120;
    const authExpiredPolicy = document.getElementById('codexPoolAuthExpiredPolicy').value || 'skip';
    const slotIds = getSelectedSlotIds();

    if (!name) {
        notify(t('codexPool.poolNameRequired'), 'error');
        return;
    }

    if (slotIds.length === 0) {
        notify(t('codexPool.poolSlotsRequired'), 'error');
        return;
    }

    try {
        let poolId = currentPoolEditId;
        if (poolId === 0) {
            const createdPool = parseJSONResult(await window.go.main.App.CreateCodexPool(name, slotIds), 'pool');
            poolId = createdPool?.id || 0;
        }

        parseJSONResult(await window.go.main.App.UpdateCodexPool(poolId, name, strategy, enabled, cooldown429Sec, cooldown5xxSec, authExpiredPolicy, slotIds), 'pool');
        closeCodexPoolModal();
        notify(t('codexPool.poolSaved'), 'success');
        await refreshDashboard();
    } catch (error) {
        console.error('Failed to save Codex pool:', error);
        notify(t('codexPool.saveFailed').replace('{error}', error.message || error), 'error');
    }
}

export async function deleteCodexPool(id) {
    const pool = codexPools.find(item => item.id === id);
    if (!pool) {
        notify(t('codexPool.poolNotFound'), 'error');
        return;
    }

    const confirmed = await confirmAction(t('codexPool.confirmDeletePool').replace('{name}', pool.name));
    if (!confirmed) {
        return;
    }

    try {
        await window.go.main.App.DeleteCodexPool(id);
        notify(t('codexPool.poolDeleted'), 'success');
        await refreshDashboard();
    } catch (error) {
        console.error('Failed to delete Codex pool:', error);
        notify(t('codexPool.deleteFailed').replace('{error}', error.message || error), 'error');
    }
}

export function getEnabledCodexPools() {
    return codexPools.filter(pool => pool.enabled !== false);
}

export function resolveEndpointCodexPoolLabel(poolId) {
    if (!poolId) {
        return '-';
    }
    return getPoolName(poolId);
}
