(() => {
  const navs = Array.from(document.querySelectorAll('.nav'));
  if (navs.length === 0) {
    return;
  }

  const nextPath = `${window.location.pathname}${window.location.search}`;
  const authAnchorSelector = 'a[href="/login"], a[href="/register"], .nav-logout-form';

  const createLink = (href, label) => {
    const link = document.createElement('a');
    link.href = href;
    link.textContent = label;
    return link;
  };

  const createNotificationsLink = (unreadCount) => {
    const link = document.createElement('a');
    link.href = '/notifications';
    link.className = 'nav-notifications-link';
    link.dataset.notificationsLink = '1';

    const label = document.createElement('span');
    label.textContent = 'Notifications';
    link.appendChild(label);

    const badge = document.createElement('span');
    badge.className = 'nav-notification-badge';
    badge.dataset.notificationsBadge = '1';
    badge.textContent = String(unreadCount || 0);
    if (!unreadCount) {
      badge.hidden = true;
    }
    link.appendChild(badge);

    return link;
  };

  const insertBeforeAuthAnchor = (nav, node) => {
    const authAnchor = nav.querySelector(authAnchorSelector);
    if (authAnchor) {
      nav.insertBefore(node, authAnchor);
      return;
    }
    nav.appendChild(node);
  };

  const ensureNotificationsLink = (nav, unreadCount) => {
    let link = nav.querySelector('[data-notifications-link="1"]');
    if (!link) {
      link = createNotificationsLink(unreadCount);
      insertBeforeAuthAnchor(nav, link);
    } else {
      const authAnchor = nav.querySelector(authAnchorSelector);
      if (authAnchor) {
        nav.insertBefore(link, authAnchor);
      }
    }

    const badge = link.querySelector('[data-notifications-badge="1"]');
    if (!badge) {
      return;
    }
    badge.textContent = String(unreadCount || 0);
    badge.hidden = !unreadCount;
  };

  const createLogoutForm = () => {
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = '/logout';
    form.className = 'nav-logout-form';

    const hidden = document.createElement('input');
    hidden.type = 'hidden';
    hidden.name = 'next';
    hidden.value = nextPath;

    const button = document.createElement('button');
    button.type = 'submit';
    button.className = 'nav-logout-btn';
    button.textContent = 'Logout';

    form.appendChild(hidden);
    form.appendChild(button);
    return form;
  };

  fetch('/api/auth/session', { credentials: 'same-origin' })
    .then((response) => (response.ok ? response.json() : null))
    .then((data) => {
      const authenticated = Boolean(data && data.authenticated);
      if (!authenticated) {
        navs.forEach((nav) => {
          const login = nav.querySelector('a[href="/login"]');
          const register = nav.querySelector('a[href="/register"]');
          const logoutForm = nav.querySelector('.nav-logout-form');

          if (logoutForm) logoutForm.remove();
          if (!login) nav.appendChild(createLink('/login', 'Login'));
          if (!register) nav.appendChild(createLink('/register', 'Register'));
          ensureNotificationsLink(nav, 0);
        });
        return null;
      }

      return fetch('/api/notifications?limit=1', { credentials: 'same-origin' })
        .then((response) => (response.ok ? response.json() : null))
        .catch(() => null)
        .then((notificationsPayload) => {
          const unreadCount = Number((notificationsPayload && notificationsPayload.unreadCount) || 0);
          navs.forEach((nav) => {
            const login = nav.querySelector('a[href="/login"]');
            const register = nav.querySelector('a[href="/register"]');
            const logoutForm = nav.querySelector('.nav-logout-form');

            if (login) login.remove();
            if (register) register.remove();
            if (!logoutForm) {
              nav.appendChild(createLogoutForm());
            }

            ensureNotificationsLink(nav, unreadCount);
          });
        });
    })
    .catch(() => {
      navs.forEach((nav) => {
        ensureNotificationsLink(nav, 0);
      });
    });
})();
