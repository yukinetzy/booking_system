(() => {
  const config = document.getElementById('hotelPresenceConfig');
  if (!config) {
    return;
  }

  const enabled = String(config.dataset.enabled || '').toLowerCase() === 'true';
  if (!enabled) {
    return;
  }

  const hotelId = String(config.dataset.hotelId || '').trim();
  if (!hotelId) {
    return;
  }

  const heartbeatSecondsRaw = Number.parseInt(config.dataset.heartbeatSeconds || '15', 10);
  const heartbeatSeconds = Number.isFinite(heartbeatSecondsRaw) && heartbeatSecondsRaw > 0
    ? heartbeatSecondsRaw
    : 15;

  const waitURL = `/hotel-wait?hotelId=${encodeURIComponent(hotelId)}`;
  let inFlight = false;

  const redirectToWait = () => {
    if (window.location.pathname === '/hotel-wait') {
      return;
    }
    window.location.href = waitURL;
  };

  const sendHeartbeat = () => {
    if (inFlight) {
      return;
    }
    inFlight = true;

    fetch(`/api/hotels/${encodeURIComponent(hotelId)}/presence/heartbeat`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
      },
      body: '{}',
    })
      .then(async (response) => {
        if (response.status === 429) {
          return { ok: true, throttled: true };
        }
        if (!response.ok) {
          throw new Error('heartbeat_failed');
        }
        return response.json().catch(() => ({ ok: false }));
      })
      .then((payload) => {
        if (!payload || payload.ok !== true) {
          redirectToWait();
        }
      })
      .catch(() => {
        // keep user on page during short network issues
      })
      .finally(() => {
        inFlight = false;
      });
  };

  sendHeartbeat();
  window.setInterval(sendHeartbeat, heartbeatSeconds * 1000);
})();
