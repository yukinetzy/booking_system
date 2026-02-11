(() => {
  const form = document.getElementById('bookingForm');
  if (!form) {
    return;
  }

  const roomInput = form.querySelector('select[name="hotelId"], select[name="roomId"], select[name="room_id"]');
  const checkInInput = form.querySelector('input[name="checkIn"]');
  const checkOutInput = form.querySelector('input[name="checkOut"]');
  if (!roomInput || !checkInInput || !checkOutInput) {
    return;
  }

  const bookingIdFromData = (form.dataset.bookingId || '').trim();
  const bookingIdFromActionMatch = (form.getAttribute('action') || '').match(/\/bookings\/([a-f0-9]{24})(?:$|\/)/i);
  const excludeBookingId = /^[a-f0-9]{24}$/i.test(bookingIdFromData)
    ? bookingIdFromData
    : (bookingIdFromActionMatch ? bookingIdFromActionMatch[1] : '');

  const popup = document.createElement('aside');
  popup.id = 'bookingBusyPopup';
  popup.className = 'toast-notice booking-popup';
  popup.setAttribute('role', 'alert');
  popup.setAttribute('aria-live', 'polite');
  popup.hidden = true;

  const popupMessage = document.createElement('p');
  popupMessage.className = 'booking-popup-message';
  popup.appendChild(popupMessage);

  const popupActions = document.createElement('div');
  popupActions.className = 'booking-popup-actions';

  const notifyButton = document.createElement('button');
  notifyButton.type = 'button';
  notifyButton.className = 'btn btn-outline btn-small';
  notifyButton.textContent = 'Notify me when available';
  popupActions.appendChild(notifyButton);

  const closeButton = document.createElement('button');
  closeButton.type = 'button';
  closeButton.className = 'btn btn-small';
  closeButton.textContent = 'Close';
  popupActions.appendChild(closeButton);

  popup.appendChild(popupActions);
  document.body.appendChild(popup);

  let latestRequestId = 0;
  let latestAvailability = null;

  const showPopup = (message, showNotifyButton, isSuccess = false) => {
    popup.hidden = false;
    popup.classList.add('visible');
    popup.classList.toggle('booking-popup-success', Boolean(isSuccess));
    popupMessage.textContent = message;
    notifyButton.hidden = !showNotifyButton;
  };

  const hidePopup = () => {
    popup.classList.remove('visible');
    popup.classList.remove('booking-popup-success');
    popup.hidden = true;
    popupMessage.textContent = '';
    notifyButton.hidden = true;
  };

  const hasFullRange = () => {
    return Boolean(roomInput.value && checkInInput.value && checkOutInput.value);
  };

  const checkAvailability = () => {
    if (!hasFullRange()) {
      latestAvailability = null;
      hidePopup();
      return;
    }

    if (checkOutInput.value <= checkInInput.value) {
      latestAvailability = null;
      hidePopup();
      return;
    }

    latestRequestId += 1;
    const requestId = latestRequestId;
    const queryPayload = {
      room_id: roomInput.value,
      check_in: checkInInput.value,
      check_out: checkOutInput.value,
    };
    if (excludeBookingId) {
      queryPayload.exclude_booking_id = excludeBookingId;
    }

    const query = new URLSearchParams(queryPayload);

    fetch(`/api/bookings/availability?${query.toString()}`, {
      credentials: 'same-origin',
    })
      .then((response) => (response.ok ? response.json() : Promise.reject(new Error('request_failed'))))
      .then((data) => {
        if (requestId !== latestRequestId) {
          return;
        }

        if (data && data.available === true) {
          latestAvailability = true;
          hidePopup();
          return;
        }

        latestAvailability = false;
        showPopup('Room is occupied for selected dates.', true);
      })
      .catch(() => {
        if (requestId !== latestRequestId) {
          return;
        }

        latestAvailability = null;
        hidePopup();
      });
  };

  const subscribeForNotification = () => {
    if (!hasFullRange()) {
      return;
    }

    notifyButton.disabled = true;
    fetch('/api/notifications/subscribe', {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        room_id: roomInput.value,
        check_in: checkInInput.value,
        check_out: checkOutInput.value,
      }),
    })
      .then(async (response) => {
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          const message = payload && payload.message ? payload.message : 'Unable to create notification subscription.';
          throw new Error(message);
        }
        return payload;
      })
      .then(() => {
        showPopup('Subscription saved. We will notify you when this room becomes available.', false, true);
      })
      .catch((error) => {
        showPopup(error.message || 'Unable to create notification subscription.', true);
      })
      .finally(() => {
        notifyButton.disabled = false;
      });
  };

  [roomInput, checkInInput, checkOutInput].forEach((node) => {
    node.addEventListener('change', checkAvailability);
    node.addEventListener('input', checkAvailability);
  });

  notifyButton.addEventListener('click', subscribeForNotification);
  closeButton.addEventListener('click', hidePopup);

  form.addEventListener('submit', (event) => {
    if (latestAvailability === false) {
      event.preventDefault();
      showPopup('Room is occupied for selected dates. Choose another range or subscribe for notification.', true);
    }
  });

  hidePopup();
  checkAvailability();
})();
