const webViewHistoryBoundaryChannelName = 'DynamicLinkHistoryBoundary';
const webViewHistoryBoundaryMarker = '__dynamicLinkOfferEntryV1';

enum WebViewBackAction { closeWebView, navigateWebViewBack, bubble }

bool isUserInitiatedMainFrameNavigation({
  required bool isMainFrame,
  required bool hasGesture,
}) => isMainFrame && hasGesture;

WebViewBackAction resolveWebViewBackAction({
  required bool protectFromHistoryTrap,
  required bool boundaryLocked,
  required bool? isCurrentEntryBoundary,
  required bool isCurrentUrlBoundary,
  required bool canGoBack,
}) {
  if (!protectFromHistoryTrap) {
    return canGoBack ? .navigateWebViewBack : .bubble;
  }

  if (!boundaryLocked ||
      isCurrentEntryBoundary == true ||
      isCurrentUrlBoundary) {
    return .closeWebView;
  }

  return canGoBack ? .navigateWebViewBack : .closeWebView;
}

String buildHistoryBoundaryScript({required bool boundaryLocked}) =>
    '''
(() => {
  const channel = window.$webViewHistoryBoundaryChannelName;
  if (!channel || window.__dynamicLinkHistoryBoundaryInstalled) {
    return;
  }

  window.__dynamicLinkHistoryBoundaryInstalled = true;

  const marker = '$webViewHistoryBoundaryMarker';
  const originalReplaceState = history.replaceState.bind(history);
  const originalPushState = history.pushState.bind(history);
  let engaged = false;
  let navigationIntent = false;
  let isBoundaryLocked = $boundaryLocked || Boolean(
    history.state && history.state[marker],
  );
  let accumulatedScroll = 0;
  let lastTouchY = null;

  const stateWithMarker = (state) => {
    const nextState = state && typeof state === 'object'
      ? {...state}
      : {};
    nextState[marker] = true;
    return nextState;
  };

  const markBoundary = () => {
    if (isBoundaryLocked) {
      return;
    }

    originalReplaceState(
      stateWithMarker(history.state),
      document.title,
      location.href,
    );
    isBoundaryLocked = true;
    try {
      channel.postMessage('locked|' + location.href);
    } catch (_) {}
  };

  const isVisible = (element) => {
    const rect = element.getBoundingClientRect();
    const style = getComputedStyle(element);
    return rect.width > 0 &&
      rect.height > 0 &&
      style.display !== 'none' &&
      style.visibility !== 'hidden' &&
      style.pointerEvents !== 'none';
  };

  const componentSelector = [
    'a[href]',
    'button',
    'input:not([type="hidden"])',
    'textarea',
    'select',
    'label',
    'summary',
    '[role="button"]',
    '[role="link"]',
    '[role="slider"]',
    '[role="scrollbar"]',
    '[onclick]',
    '[tabindex]:not([tabindex="-1"])',
  ].join(',');

  const findElement = (event) => {
    const path = typeof event.composedPath === 'function'
      ? event.composedPath()
      : [event.target];
    return path.find((item) =>
      item instanceof Element &&
      item !== document.body &&
      item !== document.documentElement &&
      isVisible(item),
    ) || null;
  };

  const registerEngagement = (event) => {
    if (!event.isTrusted) {
      return null;
    }

    const element = findElement(event);
    if (element) {
      engaged = true;
    }
    return element;
  };

  const registerComponentInteraction = (event) => {
    const element = registerEngagement(event);
    const component = element && element.closest(componentSelector);
    if (component) {
      markBoundary();
    }
    return element;
  };

  const findScrollableElement = (target) => {
    let element = target instanceof Element ? target : null;
    while (element && element !== document.body) {
      const style = getComputedStyle(element);
      const canScroll = element.scrollHeight > element.clientHeight &&
        ['auto', 'scroll'].includes(style.overflowY);
      if (canScroll && isVisible(element)) {
        return element;
      }
      element = element.parentElement;
    }

    const root = document.scrollingElement;
    return root && root.scrollHeight > root.clientHeight ? root : null;
  };

  const registerScroll = (event, distance) => {
    if (!event.isTrusted || !findScrollableElement(event.target)) {
      return;
    }

    accumulatedScroll += Math.abs(distance);
    if (accumulatedScroll >= 48) {
      engaged = true;
      markBoundary();
    }
  };

  const clearNavigationIntent = () => {
    Promise.resolve().then(() => navigationIntent = false);
  };

  document.addEventListener(
    'pointerdown',
    registerComponentInteraction,
    true,
  );
  document.addEventListener('focusin', registerEngagement, true);

  document.addEventListener('pointerup', registerComponentInteraction, true);

  document.addEventListener('input', registerComponentInteraction, true);

  document.addEventListener('click', (event) => {
    const element = registerComponentInteraction(event);
    if (!element) {
      return;
    }

    navigationIntent = true;
    const component = element.closest(componentSelector);
    if (component) {
      markBoundary();
    }
    clearNavigationIntent();
  }, true);

  document.addEventListener('submit', (event) => {
    if (!event.isTrusted && !engaged) {
      return;
    }

    const form = event.target;
    if (form instanceof HTMLFormElement && isVisible(form)) {
      navigationIntent = true;
      markBoundary();
      clearNavigationIntent();
    }
  }, true);

  document.addEventListener('wheel', (event) => {
    registerScroll(event, event.deltaY);
  }, {capture: true, passive: true});

  document.addEventListener('touchstart', (event) => {
    if (event.isTrusted && event.touches.length === 1) {
      registerComponentInteraction(event);
      lastTouchY = event.touches[0].clientY;
    }
  }, {capture: true, passive: true});

  document.addEventListener('touchmove', (event) => {
    if (lastTouchY === null || event.touches.length !== 1) {
      return;
    }

    const touchY = event.touches[0].clientY;
    registerScroll(event, touchY - lastTouchY);
    lastTouchY = touchY;
  }, {capture: true, passive: true});

  document.addEventListener('touchend', () => {
    lastTouchY = null;
  }, {capture: true, passive: true});

  window.addEventListener('beforeunload', () => {
    if (engaged && navigationIntent) {
      markBoundary();
    }
  }, true);

  history.pushState = (state, title, url) => {
    if (engaged && navigationIntent) {
      markBoundary();
    }
    return originalPushState(state, title, url);
  };

  history.replaceState = (state, title, url) => {
    if (engaged && navigationIntent) {
      markBoundary();
    }
    const shouldKeepMarker = Boolean(history.state && history.state[marker]);
    return originalReplaceState(
      shouldKeepMarker ? stateWithMarker(state) : state,
      title,
      url,
    );
  };

  if (isBoundaryLocked && history.state && history.state[marker]) {
    try {
      channel.postMessage('locked|' + location.href);
    } catch (_) {}
  }
})();
''';
