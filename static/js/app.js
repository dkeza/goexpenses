// The service worker URL carries the build version, so every deploy installs
// a fresh worker that drops the previous build's cached static files.
if ('serviceWorker' in navigator) {
  const assetVersion = new URL(document.currentScript.src).searchParams.get('v') || '';
  window.addEventListener('load', () => {
    navigator.serviceWorker.register(`/sw.js?v=${encodeURIComponent(assetVersion)}`).catch(() => {});
  });
}

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
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', next === 'dark' ? '#212529' : '#ffffff');
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

  // An IPS QR code from a bill fills the amount and description of a new post.
  const scanModalElement = document.querySelector('#ips-scan-modal');
  if (scanModalElement) {
    const scanModal = bootstrap.Modal.getOrCreateInstance(scanModalElement);
    const video = document.querySelector('#ips-scan-video');
    const status = document.querySelector('#ips-scan-status');
    const fileInput = document.querySelector('#ips-scan-file');
    const messages = scanModalElement.dataset;
    let stream = null;
    let session = 0;
    let detectorPromise = null;

    const showStatus = (text, isError = false) => {
      status.textContent = text;
      status.classList.toggle('text-danger', isError);
    };

    // BarcodeDetector is built into Chrome on Android. Other browsers, Windows among them, use jsQR.
    const getDetector = () => detectorPromise ??= (async () => {
      if ('BarcodeDetector' in window) {
        try {
          if ((await BarcodeDetector.getSupportedFormats()).includes('qr_code')) {
            const detector = new BarcodeDetector({ formats: ['qr_code'] });
            return async (source) => (await detector.detect(source)).map((code) => code.rawValue);
          }
        } catch (_) {}
      }
      await new Promise((resolve, reject) => {
        const script = document.createElement('script');
        script.src = messages.jsqrSrc;
        script.onload = resolve;
        script.onerror = reject;
        document.head.append(script);
      });
      const canvas = document.createElement('canvas');
      const context = canvas.getContext('2d', { willReadFrequently: true });
      return async (source, maxSide = 1280) => {
        const width = source.videoWidth || source.width;
        const height = source.videoHeight || source.height;
        const scale = Math.min(1, maxSide / Math.max(width, height));
        canvas.width = Math.round(width * scale);
        canvas.height = Math.round(height * scale);
        context.drawImage(source, 0, 0, canvas.width, canvas.height);
        const image = context.getImageData(0, 0, canvas.width, canvas.height);
        const code = jsQR(image.data, image.width, image.height);
        return code ? [code.data] : [];
      };
    })();

    const applyCodes = (codes) => {
      const payment = codes.map(parseIpsQr).find(Boolean);
      if (!payment) return false;
      const form = document.querySelector('#amount')?.form;
      if (form) {
        if (payment.currency === 'EUR') {
          form.elements.amounte.value = payment.amount;
          form.elements.amount.value = '';
        } else {
          form.elements.amount.value = payment.amount;
          form.elements.amounte.value = '';
        }
        if (payment.description) form.elements.description.value = payment.description;
      }
      scanModal.hide();
      form?.elements.expense_id?.focus();
      return true;
    };

    const stopCamera = () => {
      session += 1;
      stream?.getTracks().forEach((track) => track.stop());
      stream = null;
      video.srcObject = null;
      video.classList.add('d-none');
    };

    const scanFrames = async (current) => {
      if (current !== session) return;
      try {
        if (video.readyState >= video.HAVE_ENOUGH_DATA) {
          const codes = await (await getDetector())(video);
          if (current !== session) return;
          if (codes.length) {
            if (applyCodes(codes)) return;
            showStatus(messages.msgNotIps, true);
          }
        }
      } catch (_) {}
      window.setTimeout(() => scanFrames(current), 200);
    };

    const startCamera = async () => {
      const current = ++session;
      if (!navigator.mediaDevices?.getUserMedia) {
        showStatus(messages.msgNoCamera);
        return;
      }
      try {
        const cameraStream = await navigator.mediaDevices.getUserMedia({
          audio: false,
          video: { facingMode: { ideal: 'environment' }, width: { ideal: 1920 }, height: { ideal: 1080 } },
        });
        if (current !== session) {
          cameraStream.getTracks().forEach((track) => track.stop());
          return;
        }
        stream = cameraStream;
        video.srcObject = stream;
        video.classList.remove('d-none');
        await video.play();
        showStatus(messages.msgScanning);
        scanFrames(current);
      } catch (_) {
        if (current === session) showStatus(messages.msgNoCamera);
      }
    };

    const scanImage = async (file) => {
      try {
        const bitmap = await createImageBitmap(file);
        const codes = await (await getDetector())(bitmap, 2000);
        bitmap.close();
        if (!codes.length) showStatus(messages.msgNoCode, true);
        else if (!applyCodes(codes)) showStatus(messages.msgNotIps, true);
      } catch (_) {
        showStatus(messages.msgNoCode, true);
      }
    };

    scanModalElement.addEventListener('shown.bs.modal', () => {
      showStatus('');
      startCamera();
    });
    scanModalElement.addEventListener('hidden.bs.modal', stopCamera);
    fileInput.addEventListener('change', () => {
      const [file] = fileInput.files;
      fileInput.value = '';
      if (file) scanImage(file);
    });
    document.addEventListener('paste', (event) => {
      if (!scanModalElement.classList.contains('show')) return;
      const file = [...(event.clipboardData?.files || [])].find((item) => item.type.startsWith('image/'));
      if (!file) return;
      event.preventDefault();
      scanImage(file);
    });
  }
});

// Reads the NBS IPS QR payload, e.g. "K:PR|V:01|C:1|R:...|N:Payee\r\nCity|I:RSD3596,13|S:Purpose".
function parseIpsQr(text) {
  const fields = {};
  for (const part of String(text).trim().split('|')) {
    const separator = part.indexOf(':');
    if (separator > 0) fields[part.slice(0, separator).trim().toUpperCase()] = part.slice(separator + 1);
  }
  if (!['PR', 'PT', 'PK', 'EK'].includes(fields.K?.trim())) return null;
  const amountMatch = /^([A-Z]{3})(\d+)(?:,(\d{0,2}))?$/.exec((fields.I || '').trim());
  const amount = amountMatch ? Number(`${amountMatch[2]}.${amountMatch[3] || '0'}`) : 0;
  const payee = (fields.N || '').split(/\r\n|\r|\n/)[0].trim();
  const purpose = (fields.S || '').trim();
  return {
    currency: amountMatch ? amountMatch[1] : 'RSD',
    amount: amount > 0 ? String(amount) : '',
    description: [payee, purpose].filter(Boolean).join(' – ').slice(0, 200),
  };
}
