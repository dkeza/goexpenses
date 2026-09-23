document.addEventListener('DOMContentLoaded', () => {
  document.querySelectorAll('.alert-dismissible').forEach((alertElement) => {
    window.setTimeout(() => {
      if (alertElement.isConnected) {
        bootstrap.Alert.getOrCreateInstance(alertElement).close();
      }
    }, 4000);
  });

  const accountSelector = document.querySelector('#default_accounts_id');

  document.querySelectorAll('.js-confirm-admin').forEach((form) => {
    form.addEventListener('submit', (event) => {
      if (!window.confirm(form.dataset.confirmMessage || 'Confirm this action?')) {
        event.preventDefault();
      }
    });
  });
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
  const detail = document.querySelector('#delete-confirmation-detail');
  let pendingForm = null;

  if (modalElement && confirmButton) {
    const modal = bootstrap.Modal.getOrCreateInstance(modalElement);
    document.querySelectorAll('.js-confirm-delete').forEach((form) => {
      form.addEventListener('submit', (event) => {
        event.preventDefault();
        pendingForm = form;
        if (detail) detail.textContent = form.dataset.confirmDetail || '';
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
});
