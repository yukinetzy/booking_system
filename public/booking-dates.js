(() => {
  const form = document.getElementById('bookingForm');
  if (!form) {
    return;
  }

  const checkInInput = form.querySelector('input[name="checkIn"]');
  const checkOutInput = form.querySelector('input[name="checkOut"]');
  if (!checkInInput || !checkOutInput) {
    return;
  }

  const todayISO = new Date().toISOString().slice(0, 10);

  const setMinDates = () => {
    checkInInput.min = todayISO;

    if (checkInInput.value) {
      const nextDay = new Date(`${checkInInput.value}T00:00:00`);
      nextDay.setDate(nextDay.getDate() + 1);
      const nextISO = nextDay.toISOString().slice(0, 10);
      checkOutInput.min = nextISO;

      if (checkOutInput.value && checkOutInput.value <= checkInInput.value) {
        checkOutInput.value = '';
      }
    } else {
      checkOutInput.min = todayISO;
    }
  };

  const validateRange = () => {
    if (checkInInput.value && checkInInput.value < todayISO) {
      checkInInput.setCustomValidity('Check-in date must be today or later.');
      return false;
    }

    if (checkInInput.value && checkOutInput.value && checkOutInput.value <= checkInInput.value) {
      checkOutInput.setCustomValidity('Check-out date must be after check-in date.');
      return false;
    }

    checkInInput.setCustomValidity('');
    checkOutInput.setCustomValidity('');
    return true;
  };

  checkInInput.addEventListener('change', () => {
    setMinDates();
    validateRange();
    if (typeof checkOutInput.showPicker === 'function') {
      checkOutInput.showPicker();
    } else {
      checkOutInput.focus();
    }
  });

  checkOutInput.addEventListener('change', () => {
    validateRange();
  });

  form.addEventListener('submit', (event) => {
    setMinDates();
    if (!validateRange()) {
      event.preventDefault();
      form.reportValidity();
    }
  });

  setMinDates();
  validateRange();
})();
