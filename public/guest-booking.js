(() => {
  const interactiveButtons = Array.from(document.querySelectorAll('.guest-book-btn, .guest-rate-btn'));
  if (interactiveButtons.length === 0) {
    return;
  }

  const getNotice = () => {
    let node = document.getElementById('guestBookingNotice');
    if (node) return node;

    node = document.createElement('div');
    node.id = 'guestBookingNotice';
    node.className = 'toast-notice';
    node.hidden = true;
    document.body.appendChild(node);
    return node;
  };

  const showNotice = (message) => {
    const notice = getNotice();
    notice.textContent = message;
    notice.hidden = false;
    notice.classList.add('visible');
  };

  interactiveButtons.forEach((button) => {
    button.addEventListener('click', (event) => {
      const loginUrl = button.dataset.loginUrl || button.getAttribute('href') || '/login';
      const isRate = button.classList.contains('guest-rate-btn');

      event.preventDefault();
      showNotice(
        isRate
          ? 'Please sign in to rate this hotel. Redirecting to login...'
          : 'Please register or sign in to book this hotel. Redirecting to login...'
      );

      window.setTimeout(() => {
        window.location.href = loginUrl;
      }, 1000);
    });
  });
})();
