// api.js - API communication layer for SpaceMolt Fleet Command UI

const SpaceMoltAPI = (function () {
  // Base URL defaults to same origin; override via SpaceMoltAPI.setBaseURL()
  let baseURL = '';

  function setBaseURL(url) {
    baseURL = url.replace(/\/$/, '');
  }

  // Detect base URL from query param or default to current origin
  function autoDetect() {
    const params = new URLSearchParams(window.location.search);
    const apiURL = params.get('api');
    if (apiURL) {
      setBaseURL(apiURL);
    }
  }

  async function fetchJSON(path) {
    const resp = await fetch(baseURL + path);
    if (!resp.ok) {
      throw new Error(`API error: ${resp.status} ${resp.statusText}`);
    }
    return resp.json();
  }

  // GET /api/agents - list all agents
  async function listAgents() {
    return fetchJSON('/api/agents');
  }

  // GET /api/agents/{id} - get agent details
  async function getAgent(id) {
    return fetchJSON('/api/agents/' + encodeURIComponent(id));
  }

  // GET /api/agents/{id}/state - get full game state
  async function getAgentState(id) {
    return fetchJSON('/api/agents/' + encodeURIComponent(id) + '/state');
  }

  // GET /api/agents/{id}/history?limit=N
  async function getAgentHistory(id, limit) {
    limit = limit || 50;
    return fetchJSON('/api/agents/' + encodeURIComponent(id) + '/history?limit=' + limit);
  }

  // Subscribe to agent SSE stream, returns { close() } handle
  function subscribeAgent(id, onEvent) {
    const url = baseURL + '/api/agents/' + encodeURIComponent(id) + '/stream';
    const source = new EventSource(url);

    source.addEventListener('connected', function (e) {
      onEvent('connected', JSON.parse(e.data));
    });

    source.addEventListener('state_update', function (e) {
      onEvent('state_update', JSON.parse(e.data));
    });

    source.addEventListener('action', function (e) {
      onEvent('action', JSON.parse(e.data));
    });

    source.addEventListener('decision', function (e) {
      onEvent('decision', JSON.parse(e.data));
    });

    source.addEventListener('error', function () {
      onEvent('error', { message: 'SSE connection lost' });
    });

    return {
      close: function () {
        source.close();
      }
    };
  }

  // Fetch state for all agents in parallel
  async function getAllAgentStates() {
    const agents = await listAgents();
    const results = await Promise.allSettled(
      agents.map(async function (a) {
        try {
          const detail = await getAgent(a.id);
          let state = null;
          try {
            state = await getAgentState(a.id);
          } catch (_) {
            // state may not be available yet
          }
          return { agent: detail, state: state };
        } catch (err) {
          return { agent: a, state: null, error: err.message };
        }
      })
    );
    return results
      .filter(function (r) { return r.status === 'fulfilled'; })
      .map(function (r) { return r.value; });
  }

  autoDetect();

  return {
    setBaseURL: setBaseURL,
    listAgents: listAgents,
    getAgent: getAgent,
    getAgentState: getAgentState,
    getAgentHistory: getAgentHistory,
    subscribeAgent: subscribeAgent,
    getAllAgentStates: getAllAgentStates,
  };
})();
