(function () {
  'use strict';

  var STRIPE_JS_URL = 'https://js.stripe.com/v3/';

  function loadStripeJS() {
    if (window.Stripe) return Promise.resolve(window.Stripe);
    return new Promise(function (resolve, reject) {
      var s = document.createElement('script');
      s.src = STRIPE_JS_URL;
      s.onload = function () { resolve(window.Stripe); };
      s.onerror = function () { reject(new Error('Failed to load Stripe.js')); };
      document.head.appendChild(s);
    });
  }

  var CSS_CLASS = 'stoa-stripe-checkout';

  // Inject scoped styles once into the document head.
  var stylesInjected = false;
  function injectStyles() {
    if (stylesInjected) return;
    stylesInjected = true;
    var style = document.createElement('style');
    style.textContent = [
      '.' + CSS_CLASS + ' { display: block; }',
      '.' + CSS_CLASS + ' .stripe-container { padding: 1.5rem; border: 1px solid #e5e7eb; border-radius: 0.75rem; background: #fff; }',
      '.' + CSS_CLASS + ' .stripe-header { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 1rem; }',
      '.' + CSS_CLASS + ' .stripe-header svg { flex-shrink: 0; }',
      '.' + CSS_CLASS + ' .stripe-header h3 { font-size: 1rem; font-weight: 600; color: #111827; margin: 0; }',
      '.' + CSS_CLASS + ' .stripe-mount { min-height: 80px; }',
      '.' + CSS_CLASS + ' .stripe-error { color: #dc2626; font-size: 0.875rem; margin-top: 0.5rem; display: none; }',
      '.' + CSS_CLASS + ' .stripe-btn { display: flex; align-items: center; justify-content: center; gap: 0.5rem; width: 100%; margin-top: 1rem; padding: 0.75rem 1.5rem; font-size: 0.9375rem; font-weight: 600; color: #fff; background: #635bff; border: none; border-radius: 0.5rem; cursor: pointer; transition: background 0.15s; }',
      '.' + CSS_CLASS + ' .stripe-btn:hover:not(:disabled) { background: #4f46e5; }',
      '.' + CSS_CLASS + ' .stripe-btn:disabled { opacity: 0.6; cursor: not-allowed; }',
      '.' + CSS_CLASS + ' .stripe-spinner { width: 1.25rem; height: 1.25rem; border: 2px solid rgba(255,255,255,0.3); border-top-color: #fff; border-radius: 50%; animation: stoa-stripe-spin 0.6s linear infinite; }',
      '@keyframes stoa-stripe-spin { to { transform: rotate(360deg); } }',
      '.' + CSS_CLASS + ' .stripe-loading { display: flex; align-items: center; justify-content: center; padding: 2rem; color: #6b7280; font-size: 0.875rem; gap: 0.5rem; }'
    ].join('\n');
    document.head.appendChild(style);
  }

  function createSVGIcon() {
    var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('width', '20');
    svg.setAttribute('height', '20');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('fill', 'none');
    var path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', 'M13.976 9.15c-2.172-.806-3.356-1.426-3.356-2.409 0-.831.683-1.305 1.901-1.305 2.227 0 4.515.858 6.09 1.631l.89-5.494C18.252.975 15.697 0 12.165 0 9.667 0 7.589.654 6.104 1.872 4.56 3.147 3.757 4.992 3.757 7.218c0 4.039 2.467 5.76 6.476 7.219 2.585.92 3.445 1.574 3.445 2.583 0 .98-.84 1.545-2.354 1.545-1.875 0-4.965-.921-6.99-2.109l-.9 5.555C5.175 22.99 8.385 24 11.714 24c2.641 0 4.843-.624 6.328-1.813 1.664-1.305 2.525-3.236 2.525-5.732 0-4.128-2.524-5.851-6.591-7.305z');
    path.setAttribute('fill', '#635BFF');
    svg.appendChild(path);
    return svg;
  }

  function el(tag, attrs, children) {
    var node = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (k) {
        if (k === 'className') node.className = attrs[k];
        else node.setAttribute(k, attrs[k]);
      });
    }
    if (children) {
      children.forEach(function (c) {
        if (typeof c === 'string') node.appendChild(document.createTextNode(c));
        else if (c) node.appendChild(c);
      });
    }
    return node;
  }

  class StoaStripeCheckout extends HTMLElement {
    constructor() {
      super();
      this._initialized = false;
      this._context = null;
      this._apiClient = null;
    }

    set context(val) {
      this._context = val;
      this._tryInit();
    }

    set apiClient(val) {
      this._apiClient = val;
      this._tryInit();
    }

    connectedCallback() {
      this._tryInit();
    }

    _tryInit() {
      if (this._initialized || !this._context || !this._apiClient || !this.isConnected) return;
      if (!this._context.orderId) return;
      this._initialized = true;

      injectStyles();
      this.classList.add(CSS_CLASS);

      var container = el('div', { className: 'stripe-container' });
      var loadingEl = el('div', { className: 'stripe-loading' }, [
        el('div', { className: 'stripe-spinner' }),
        'Payment wird geladen\u2026'
      ]);
      container.appendChild(loadingEl);
      this.appendChild(container);

      this._initPayment(container).catch(function (err) {
        container.textContent = '';
        var errMsg = el('div', { className: 'stripe-loading', style: 'color:#dc2626;' }, [
          'Payment konnte nicht geladen werden.'
        ]);
        container.appendChild(errMsg);
        this._dispatch('payment-error', { message: err.message || 'Initialization failed' });
      }.bind(this));
    }

    async _initPayment(container) {
      var body = {
        order_id: this._context.orderId,
        payment_method_id: this._context.paymentMethodId
      };
      if (this._context.guestToken) {
        body.guest_token = this._context.guestToken;
      }
      var data = await this._apiClient.post('/store/stripe/payment-intent', body);

      if (!data || !data.client_secret) {
        throw new Error('Invalid payment intent response');
      }

      var StripeConstructor = await loadStripeJS();
      var stripe = StripeConstructor(data.publishable_key);
      var elements = stripe.elements({
        clientSecret: data.client_secret,
        appearance: {
          theme: 'stripe',
          variables: {
            colorPrimary: '#635bff',
            borderRadius: '6px',
            fontFamily: 'system-ui, -apple-system, sans-serif'
          }
        }
      });
      var paymentElement = elements.create('payment');

      // Build UI — all in Light DOM so Stripe can find the mount point.
      container.textContent = '';

      var header = el('div', { className: 'stripe-header' }, [
        createSVGIcon(),
        el('h3', null, ['Kartenzahlung'])
      ]);
      container.appendChild(header);

      var mountPoint = el('div', { className: 'stripe-mount' });
      container.appendChild(mountPoint);

      var errorEl = el('div', { className: 'stripe-error' });
      container.appendChild(errorEl);

      var spinner = el('div', { className: 'stripe-spinner' });
      var btnText = document.createTextNode('Jetzt bezahlen');
      var btn = el('button', { className: 'stripe-btn', type: 'button' }, [btnText]);
      btn.disabled = true;
      container.appendChild(btn);

      paymentElement.mount(mountPoint);

      paymentElement.on('ready', function () {
        btn.disabled = false;
      });

      var self = this;
      btn.addEventListener('click', async function () {
        btn.disabled = true;
        btn.textContent = '';
        btn.appendChild(spinner.cloneNode(true));
        btn.appendChild(document.createTextNode('Wird verarbeitet\u2026'));
        errorEl.style.display = 'none';

        var result = await stripe.confirmPayment({
          elements: elements,
          confirmParams: {
            return_url: window.location.origin + '/checkout/success?order=' + encodeURIComponent(self._context.orderNumber || '')
          },
          redirect: 'if_required'
        });

        if (result.error) {
          errorEl.textContent = result.error.message;
          errorEl.style.display = 'block';
          btn.disabled = false;
          btn.textContent = 'Jetzt bezahlen';
          self._dispatch('payment-error', { message: result.error.message });
        } else {
          self._dispatch('payment-success', {
            paymentIntentId: result.paymentIntent ? result.paymentIntent.id : null
          });
        }
      });
    }

    _dispatch(type, detail) {
      this.dispatchEvent(new CustomEvent('plugin-event', {
        bubbles: true,
        composed: true,
        detail: Object.assign({ type: type }, detail)
      }));
    }
  }

  customElements.define('stoa-stripe-checkout', StoaStripeCheckout);
})();
