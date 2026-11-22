// Progressive enhancement: navigation, commands and image links work without JS.
const header = document.querySelector('.site-header');
const toggle = document.querySelector('.menu-toggle');
const nav = document.querySelector('#main-nav');
const mobile = window.matchMedia('(max-width: 850px)');

function closeMenu(returnFocus = false) {
  toggle.setAttribute('aria-expanded', 'false');
  nav.classList.remove('is-open');
  if (returnFocus) toggle.focus();
}

header.classList.add('is-enhanced');
toggle.hidden = !mobile.matches;
mobile.addEventListener('change', () => {
  toggle.hidden = !mobile.matches;
  closeMenu();
});
toggle.addEventListener('click', () => {
  const open = toggle.getAttribute('aria-expanded') !== 'true';
  toggle.setAttribute('aria-expanded', String(open));
  nav.classList.toggle('is-open', open);
});
header.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && toggle.getAttribute('aria-expanded') === 'true') closeMenu(true);
});
nav.addEventListener('click', (event) => {
  const link = event.target.closest('a');
  if (!link || !mobile.matches) return;
  closeMenu();
  // Keep keyboard focus with the destination after collapsing navigation.
  if (link.hash && link.origin === location.origin && link.pathname === location.pathname) {
    const target = document.getElementById(link.hash.slice(1));
    if (target) {
      target.tabIndex = -1;
      target.focus({ preventScroll: true });
    }
  }
});

for (const button of document.querySelectorAll('[data-copy], [data-copy-shown]')) {
  button.hidden = false;
  button.addEventListener('click', async () => {
    // data-copy-shown: the one block of that class the quickstart's level and switches show.
    const code = button.dataset.copyShown
      ? [...document.querySelectorAll(`.${button.dataset.copyShown}`)].find((block) => block.offsetParent)?.querySelector('code')
      : document.getElementById(button.dataset.copy);
    const status = button.closest('.quickstart-install')?.querySelector('.copy-status') ?? document.getElementById('copy-status');
    try {
      await navigator.clipboard.writeText(code.textContent.trim());
      status.textContent = 'Commands copied. Paste them into your terminal.';
    } catch {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(code);
      selection.removeAllRanges();
      selection.addRange(range);
      status.textContent = 'Clipboard unavailable. Commands selected; use your device’s copy action.';
    }
  });
}

// The quickstart's levels: a link to #intermediate (from a guide's "Then", or another page) picks
// that level, whose guide is hidden until its radio is checked. Without JS the level cards still do it.
function pickLevel() {
  const level = document.getElementById(`lvl-${location.hash.slice(1)}`);
  if (!level) return;
  level.checked = true;
  document.getElementById(location.hash.slice(1))?.scrollIntoView();
}
window.addEventListener('hashchange', pickLevel);
pickLevel();

// Screenshots open in a viewer over the page instead of a new page: the full capture, scrollable,
// with a close button. Without JS (or with a modifier key, for a new tab) the links still open the PNG.
const shotLinks = [...document.querySelectorAll('a[href^="/screenshots/"][href$=".png"]')];
if (shotLinks.length && typeof HTMLDialogElement === 'function') {
  const viewer = document.createElement('dialog');
  viewer.className = 'shot-viewer';
  viewer.setAttribute('aria-label', 'Screenshot');
  viewer.innerHTML = `
    <div class="shot-viewer-bar">
      <p class="shot-viewer-title"></p>
      <a class="shot-viewer-open" target="_blank" rel="noopener">Open in new tab ↗</a>
      <button type="button" class="shot-viewer-close" aria-label="Close">×</button>
    </div>
    <div class="shot-viewer-body"><img alt=""></div>`;
  document.body.append(viewer);
  const body = viewer.querySelector('.shot-viewer-body');
  const image = viewer.querySelector('img');
  const title = viewer.querySelector('.shot-viewer-title');
  const openTab = viewer.querySelector('.shot-viewer-open');
  let opener = null;

  // Back (a phone's back gesture too) closes the viewer rather than leaving the page.
  const close = () => {
    if (history.state?.shotViewer) history.back();
    else viewer.close();
  };
  window.addEventListener('popstate', () => { if (viewer.open) viewer.close(); });
  viewer.addEventListener('close', () => {
    document.documentElement.classList.remove('has-viewer');
    if (history.state?.shotViewer) history.back();
    image.removeAttribute('src');
    opener?.focus({ preventScroll: true });
  });
  viewer.querySelector('.shot-viewer-close').addEventListener('click', close);
  // A click on the dimmed area around the image closes it.
  body.addEventListener('click', (event) => { if (event.target === body) close(); });
  // Click the image to switch between fitting the window's width and full resolution.
  image.addEventListener('click', () => {
    if (image.naturalWidth > body.clientWidth || viewer.classList.contains('is-zoomed')) viewer.classList.toggle('is-zoomed');
  });

  for (const link of shotLinks) {
    link.addEventListener('click', (event) => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      opener = link;
      const figure = link.closest('figure');
      const preview = figure?.querySelector('img');
      // A gallery card's heading, else the link's label without its "Open the full screenshot:".
      const heading = figure?.querySelector('h3, figcaption strong')?.textContent;
      const label = (figure?.querySelector('a[aria-label]') ?? link).getAttribute('aria-label');
      const name = (heading ?? label?.replace(/^Open the full (\w+:\s*)?/, ''))?.trim();
      title.textContent = name || 'Screenshot';
      viewer.setAttribute('aria-label', name || 'Screenshot');
      image.alt = preview?.alt ?? '';
      // Show the web-sized copy at once, then the full capture when it arrives.
      image.src = preview?.currentSrc || link.href;
      if (preview) {
        const full = new Image();
        full.onload = () => { if (viewer.open && opener === link) image.src = link.href; };
        full.src = link.href;
      }
      openTab.href = link.href;
      viewer.classList.remove('is-zoomed');
      viewer.showModal();
      document.documentElement.classList.add('has-viewer');
      body.scrollTop = 0;
      body.scrollLeft = 0;
      history.pushState({ shotViewer: true }, '');
    });
  }
}
