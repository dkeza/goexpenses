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
});
