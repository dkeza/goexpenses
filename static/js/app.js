document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('.alert-dismissible').forEach((alertElement) => {
    window.setTimeout(() => {
      if (alertElement.isConnected) {
        bootstrap.Alert.getOrCreateInstance(alertElement).close();
      }
    }, 4000);
  });

  const accountSelector = document.querySelector('#default_accounts_id');

  accountSelector?.addEventListener('change', () => {
    document.querySelector('#account-selector')?.requestSubmit();
  });

  const themeToggle = document.querySelector('#theme-toggle');
  themeToggle?.addEventListener('click', () => {
    const current = document.documentElement.getAttribute('data-bs-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-bs-theme', next);
    try { localStorage.setItem('theme', next); } catch (_) {}
  });

  const modalElement = document.querySelector('#delete-confirmation');
  const confirmButton = document.querySelector('#delete-confirmation-submit');
  const title = document.querySelector('#delete-confirmation-title');
  const message = document.querySelector('#delete-confirmation-message');
  const detail = document.querySelector('#delete-confirmation-detail');
  let pendingForm = null;

  if (modalElement && confirmButton) {
    const modal = bootstrap.Modal.getOrCreateInstance(modalElement);
    const defaultTitle = title?.textContent || '';
    const defaultMessage = message?.textContent || '';
    const defaultButtonText = confirmButton.textContent;
    document.querySelectorAll('.js-confirm-delete, .js-confirm-admin').forEach((form) => {
      form.addEventListener('submit', (event) => {
        event.preventDefault();
        pendingForm = form;
        if (title) title.textContent = form.dataset.confirmTitle || defaultTitle;
        if (message) message.textContent = form.dataset.confirmMessage || defaultMessage;
        if (detail) detail.textContent = form.dataset.confirmDetail || '';
        confirmButton.textContent = form.classList.contains('js-confirm-admin')
          ? form.querySelector('button[type="submit"]')?.textContent || defaultButtonText
          : defaultButtonText;
        confirmButton.classList.toggle('btn-success', form.dataset.confirmTone === 'success');
        confirmButton.classList.toggle('btn-danger', form.dataset.confirmTone !== 'success');
        modal.show();
      });
    });
    confirmButton.addEventListener('click', () => {
      if (!pendingForm) return;
      const form = pendingForm;
      pendingForm = null;
      modal.hide();
      form.submit();
    });
    modalElement.addEventListener('hidden.bs.modal', () => {
      pendingForm = null;
    });
  }

  // Quick periods fill the date fields of the posts filter.
  const isoDate = (date) => {
    const pad = (value) => String(value).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  };
  const filterPeriods = {
    'this-month': (today) => [new Date(today.getFullYear(), today.getMonth(), 1), new Date(today.getFullYear(), today.getMonth() + 1, 0)],
    'last-month': (today) => [new Date(today.getFullYear(), today.getMonth() - 1, 1), new Date(today.getFullYear(), today.getMonth(), 0)],
    'last-30-days': (today) => [new Date(today.getFullYear(), today.getMonth(), today.getDate() - 29), today],
    'this-year': (today) => [new Date(today.getFullYear(), 0, 1), new Date(today.getFullYear(), 11, 31)],
    'last-year': (today) => [new Date(today.getFullYear() - 1, 0, 1), new Date(today.getFullYear() - 1, 11, 31)],
  };
  document.querySelectorAll('[data-filter-period]').forEach((button) => {
    button.addEventListener('click', () => {
      const period = filterPeriods[button.dataset.filterPeriod];
      const form = button.closest('form');
      if (!period || !form) return;
      const [from, to] = period(new Date());
      form.elements.from.value = isoDate(from);
      form.elements.to.value = isoDate(to);
    });
  });
});
