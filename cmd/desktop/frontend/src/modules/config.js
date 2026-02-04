// Configuration management
export async function loadConfig() {
    try {
        if (!window.go?.main?.App) {
            console.error('Not running in Wails environment');
            document.getElementById('endpointList').innerHTML = `
                <div class="empty-state">
                    <p>⚠️ Please run this app through Wails</p>
                    <p>Use: wails dev or run the built application</p>
                </div>
            `;
            return null;
        }

        const configStr = await window.go.main.App.GetConfig();
        const config = JSON.parse(configStr);

        const claudePort = config.claudePort || 3000;
        const codexPort = config.codexPort || 3001;
        document.getElementById('claudePort').textContent = claudePort;
        document.getElementById('codexPort').textContent = codexPort;
        document.getElementById('totalEndpoints').textContent = config.endpoints.length;

        const activeCount = config.endpoints.filter(ep => ep.enabled !== false).length;
        document.getElementById('activeEndpoints').textContent = activeCount;

        window.latestConfig = config;
        return config;
    } catch (error) {
        console.error('Failed to load config:', error);
        return null;
    }
}

export async function updatePorts(claudePort, codexPort) {
    await window.go.main.App.UpdatePorts(claudePort, codexPort);
}

export async function addEndpoint(name, url, key, transformer, model, remark, proxyUrl, clientType) {
    await window.go.main.App.AddEndpoint(name, url, key, transformer, model, remark || '', proxyUrl || '', clientType || '');
}

export async function updateEndpoint(id, name, url, key, transformer, model, remark, proxyUrl, clientType) {
    await window.go.main.App.UpdateEndpoint(id, name, url, key, transformer, model, remark || '', proxyUrl || '', clientType || '');
}

export async function removeEndpoint(id) {
    await window.go.main.App.RemoveEndpoint(id);
}

export async function toggleEndpoint(id, enabled) {
    await window.go.main.App.ToggleEndpoint(id, enabled);
}

export async function testEndpoint(id) {
    const resultStr = await window.go.main.App.TestEndpoint(id);
    return JSON.parse(resultStr);
}

export async function testEndpointLight(id) {
    const resultStr = await window.go.main.App.TestEndpointLight(id);
    return JSON.parse(resultStr);
}

export async function testAllEndpointsZeroCost() {
    const resultStr = await window.go.main.App.TestAllEndpointsZeroCost();
    return JSON.parse(resultStr);
}
