(() => {
  const config = document.getElementById('hotelWaitConfig');
  if (!config) {
    return;
  }

  const hotelId = String(config.dataset.hotelId || '').trim();
  if (!hotelId) {
    return;
  }

  const pollSecondsRaw = Number.parseInt(config.dataset.pollSeconds || '4', 10);
  const pollSeconds = Number.isFinite(pollSecondsRaw) && pollSecondsRaw > 0
    ? pollSecondsRaw
    : 4;

  const statusNode = document.getElementById('hotelWaitStatus');
  let inFlight = false;

  const updateStatusText = (active, capacity) => {
    if (!statusNode) {
      return;
    }
    statusNode.textContent = `The hotel is currently viewed by ${active} user(s). Capacity is ${capacity}. This page will refresh automatically.`;
  };

  const checkStatus = () => {
    if (inFlight) {
      return;
    }
    inFlight = true;

    fetch(`/api/hotels/${encodeURIComponent(hotelId)}/presence/status`, {
      credentials: 'same-origin',
    })
      .then((response) => {
        if (response.status === 429) {
          return null;
        }
        if (!response.ok) {
          throw new Error('status_failed');
        }
        return response.json();
      })
      .then((payload) => {
        if (!payload) {
          return;
        }
        const active = Number(payload.active || 0);
        const capacity = Number(payload.capacity || 1);
        updateStatusText(active, capacity);

        if (payload.can_enter === true) {
          window.location.href = `/hotels/${encodeURIComponent(hotelId)}`;
        }
      })
      .catch(() => {
        // transient issues should not break waiting page
      })
      .finally(() => {
        inFlight = false;
      });
  };

  checkStatus();
  window.setInterval(checkStatus, pollSeconds * 1000);
})();
