(() => {
  const app = document.getElementById('notificationsApp');
  if (!app) {
    return;
  }

  const listNode = document.getElementById('notificationsList');
  const unreadNode = document.getElementById('notificationsUnreadCount');
  const readAllButton = document.getElementById('notificationsReadAllButton');
  if (!listNode || !unreadNode || !readAllButton) {
    return;
  }

  let items = [];

  const formatDate = (value) => {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    return date.toLocaleString();
  };

  const render = (unreadCount) => {
    unreadNode.textContent = String(unreadCount);

    if (items.length === 0) {
      listNode.innerHTML = '<div class="notification-empty">No notifications yet.</div>';
      return;
    }

    listNode.innerHTML = items.map((item) => {
      const linkHtml = item.link
        ? `<a class="btn btn-outline btn-small" href="${item.link}">Open</a>`
        : '';
      const readButton = item.isRead
        ? ''
        : `<button class="btn btn-outline btn-small notification-read-btn" data-id="${item.id}" type="button">Mark as read</button>`;

      return `
        <article class="notification-item ${item.isRead ? 'read' : 'unread'}">
          <h3>${item.title || 'Notification'}</h3>
          <p>${item.text || ''}</p>
          <div class="notification-meta">
            <span>${formatDate(item.createdAt)}</span>
            <div class="notification-actions">
              ${linkHtml}
              ${readButton}
            </div>
          </div>
        </article>
      `;
    }).join('');
  };

  const loadNotifications = () => {
    fetch('/api/notifications', { credentials: 'same-origin' })
      .then((response) => {
        if (response.status === 401) {
          return { __unauthorized: true };
        }
        if (!response.ok) {
          throw new Error('Failed to load notifications');
        }
        return response.json();
      })
      .then((payload) => {
        if (payload && payload.__unauthorized) {
          unreadNode.textContent = '0';
          readAllButton.disabled = true;
          listNode.innerHTML = '<div class="notification-empty">Please <a href="/login?next=%2Fnotifications">login</a> to view your notifications.</div>';
          return;
        }

        readAllButton.disabled = false;
        items = Array.isArray(payload.items) ? payload.items : [];
        render(Number(payload.unreadCount || 0));
      })
      .catch(() => {
        readAllButton.disabled = false;
        listNode.innerHTML = '<div class="notification-empty">Unable to load notifications right now.</div>';
      });
  };

  const markAsRead = (id) => {
    fetch(`/api/notifications/${encodeURIComponent(id)}/read`, {
      method: 'POST',
      credentials: 'same-origin',
    }).then(() => {
      loadNotifications();
    });
  };

  const markAllAsRead = () => {
    readAllButton.disabled = true;
    fetch('/api/notifications/read-all', {
      method: 'POST',
      credentials: 'same-origin',
    })
      .then(() => {
        loadNotifications();
      })
      .finally(() => {
        readAllButton.disabled = false;
      });
  };

  listNode.addEventListener('click', (event) => {
    const button = event.target.closest('.notification-read-btn');
    if (!button) {
      return;
    }
    const id = button.dataset.id;
    if (!id) {
      return;
    }
    markAsRead(id);
  });

  readAllButton.addEventListener('click', markAllAsRead);

  loadNotifications();
})();
