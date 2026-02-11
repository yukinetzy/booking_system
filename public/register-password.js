(() => {
  const form = document.getElementById('registerForm');
  if (!form) {
    return;
  }

  const emailInput = document.getElementById('email');
  const passwordInput = document.getElementById('password');
  const confirmInput = document.getElementById('confirmPassword');
  const termsInput = document.getElementById('terms');
  const hints = document.getElementById('passwordHints');

  const rules = {
    lengthRule: document.getElementById('ruleLength'),
    lowerRule: document.getElementById('ruleLower'),
    upperRule: document.getElementById('ruleUpper'),
    digitRule: document.getElementById('ruleDigit'),
    specialRule: document.getElementById('ruleSpecial'),
    overlapRule: document.getElementById('ruleNoOverlap')
  };

  const hasThreeCharOverlap = (password, reference) => {
    const pass = String(password || '').toLowerCase();
    const ref = String(reference || '').toLowerCase();

    if (pass.length < 3 || ref.length < 3) {
      return false;
    }

    for (let i = 0; i <= ref.length - 3; i += 1) {
      const fragment = ref.slice(i, i + 3);
      if (!/^[a-z0-9]{3}$/.test(fragment)) {
        continue;
      }
      if (pass.includes(fragment)) {
        return true;
      }
    }

    return false;
  };

  const getReferenceToken = () => {
    const email = (emailInput.value || '').trim().toLowerCase();
    return email ? (email.split('@')[0] || email) : '';
  };

  const evaluatePassword = (password) => {
    const text = String(password || '');
    const specialCount = (text.match(/[^a-zA-Z0-9]/g) || []).length;

    return {
      lengthRule: text.length >= 8 && text.length <= 50,
      lowerRule: /[a-z]/.test(text),
      upperRule: /[A-Z]/.test(text),
      digitRule: /[0-9]/.test(text),
      specialRule: specialCount >= 1 && specialCount <= 10,
      overlapRule: !hasThreeCharOverlap(text, getReferenceToken())
    };
  };

  const setRuleState = (element, valid) => {
    if (!element) return;
    element.classList.toggle('valid', valid);
    element.classList.toggle('invalid', !valid);
  };

  const updateRules = () => {
    const result = evaluatePassword(passwordInput.value);
    Object.keys(rules).forEach((key) => {
      setRuleState(rules[key], result[key]);
    });

    const allValid = Object.values(result).every(Boolean);
    passwordInput.setCustomValidity(allValid ? '' : 'Password does not meet security requirements.');
    return allValid;
  };

  const updateConfirm = () => {
    const same = confirmInput.value === passwordInput.value;
    confirmInput.setCustomValidity(same ? '' : 'Password confirmation does not match.');
    return same;
  };

  const updateTerms = () => {
    termsInput.setCustomValidity(termsInput.checked ? '' : 'You must accept the terms to continue.');
    return termsInput.checked;
  };

  passwordInput.addEventListener('focus', () => {
    hints.classList.add('visible');
  });

  passwordInput.addEventListener('blur', () => {
    setTimeout(() => {
      if (document.activeElement !== passwordInput) {
        hints.classList.remove('visible');
      }
    }, 120);
  });

  [passwordInput, emailInput].forEach((element) => {
    element.addEventListener('input', () => {
      updateRules();
      updateConfirm();
    });
  });

  confirmInput.addEventListener('input', updateConfirm);
  termsInput.addEventListener('change', updateTerms);

  form.addEventListener('submit', (event) => {
    const ok = updateRules() && updateConfirm() && updateTerms();
    if (!ok) {
      event.preventDefault();
      form.reportValidity();
      hints.classList.add('visible');
    }
  });

  updateRules();
})();
