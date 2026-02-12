(() => {
  const form = document.getElementById('bookingForm');
  if (!form) return;

  const roomInput = form.querySelector('select[name="hotelId"], select[name="roomId"], select[name="room_id"]');
  const checkInInput = form.querySelector('input[name="checkIn"], input[name="check_in"]');
  const checkOutInput = form.querySelector('input[name="checkOut"], input[name="check_out"]');
  if (!roomInput || !checkInInput || !checkOutInput) return;

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

  const notifyPriorityButton = document.createElement('button');
  notifyPriorityButton.type = 'button';
  notifyPriorityButton.className = 'btn btn-outline btn-small';
  notifyPriorityButton.textContent = 'Priority notify';
  notifyPriorityButton.hidden = true;

  const closeButton = document.createElement('button');
  closeButton.type = 'button';
  closeButton.className = 'btn btn-small';
  closeButton.textContent = 'Close';

  popupActions.appendChild(notifyPriorityButton);
  popupActions.appendChild(closeButton);

  popup.appendChild(popupActions);
  document.body.appendChild(popup);

  closeButton.addEventListener('click', () => {
    hidePopup();
  });

  let latestRequestId = 0;
  let latestAvailability = null;

  const showPopup = (message, showPriorityButton, isSuccess = false) => {
    popup.hidden = false;
    popup.classList.add('visible');
    popup.classList.toggle('booking-popup-success', Boolean(isSuccess));
    popupMessage.textContent = message;
    notifyPriorityButton.hidden = !showPriorityButton;
  };

  const hidePopup = () => {
    popup.classList.remove('visible');
    popup.classList.remove('booking-popup-success');
    popup.hidden = true;
    popupMessage.textContent = '';
    notifyPriorityButton.hidden = true;
  };

  const hasFullRange = () => Boolean(roomInput.value && checkInInput.value && checkOutInput.value);

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
    if (excludeBookingId) queryPayload.exclude_booking_id = excludeBookingId;

    const query = new URLSearchParams(queryPayload);

    fetch(`/api/bookings/availability?${query.toString()}`, { credentials: 'same-origin' })
      .then((response) => (response.ok ? response.json() : Promise.reject(new Error('request_failed'))))
      .then((data) => {
        if (requestId !== latestRequestId) return;

        if (data && data.available === true) {
          latestAvailability = true;
          hidePopup();
          return;
        }

        latestAvailability = false;
        showPopup('Room is occupied for selected dates.', true);
      })
      .catch(() => {
        if (requestId !== latestRequestId) return;
        latestAvailability = null;
        hidePopup();
      });
  };

  const subscribePriority = () => {
    if (!hasFullRange()) return;

    notifyPriorityButton.disabled = true;

    fetch('/api/notifications/subscribe', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        room_id: roomInput.value,
        check_in: checkInInput.value,
        check_out: checkOutInput.value,
        type: 'priority',
      }),
    })
      .then(async (response) => {
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          const code = payload && payload.error ? payload.error : 'request_failed';
          throw new Error(code);
        }
        return payload;
      })
      .then((payload) => {
        if (payload && payload.group_id) {
          const groupInput = document.getElementById('groupId');
          if (groupInput) groupInput.value = payload.group_id;
        }

        showPopup('Priority subscription saved. We will notify you first when the slot becomes available.', false, true);
      })
      .catch((error) => {
        const msg =
          error.message === 'priority_taken'
            ? 'Priority slot is already taken by another user.'
            : error.message === 'duplicate_subscription'
            ? 'You already have an active subscription for these dates.'
            : 'Unable to create priority notification subscription.';
        showPopup(msg, true);
      })
      .finally(() => {
        notifyPriorityButton.disabled = false;
      });
  };

  [roomInput, checkInInput, checkOutInput].forEach((node) => {
    node.addEventListener('change', checkAvailability);
    node.addEventListener('input', checkAvailability);
  });

  notifyPriorityButton.addEventListener('click', subscribePriority);

  form.addEventListener('submit', (event) => {
    if (latestAvailability === false) {
      event.preventDefault();
      showPopup('Room is occupied for selected dates. Choose another range or subscribe for notification.', true);
    }
  });

  hidePopup();
  checkAvailability();
})();
