// GitHub links: every [data-repo] gets window.REPO + its suffix.
document.querySelectorAll('[data-repo]').forEach((a) => {
  a.href = window.REPO + a.dataset.repo;
});

// Hero screenshot switcher (OS × color scheme).
const HERO = {
  'mac-light': 'images/history.webp',
  'mac-dark': 'images/history-dark.webp',
  'win-light': 'images/windows-history.webp',
  'win-dark': 'images/windows-overview-dark.webp',
  'linux-light': 'images/linux-history.webp',
  'linux-dark': 'images/linux-history-dark.webp',
};
const state = {
  os: /Windows/.test(navigator.userAgent) ? 'win' : /Linux|X11|CrOS/.test(navigator.userAgent) && !/Android/.test(navigator.userAgent) ? 'linux' : 'mac',
  scheme: matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light',
};
const heroImg = document.getElementById('hero-img');
function renderHero() {
  document.querySelectorAll('.seg').forEach((seg) => {
    seg.querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.val === state[seg.dataset.group]));
  });
  const src = HERO[`${state.os}-${state.scheme}`];
  if (heroImg.getAttribute('src') === src) return;
  heroImg.style.opacity = '0';
  const next = new Image();
  next.onload = () => { heroImg.src = src; heroImg.style.opacity = '1'; };
  next.src = src;
}
document.querySelectorAll('.seg').forEach((seg) => {
  seg.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    state[seg.dataset.group] = b.dataset.val;
    renderHero();
  });
});
renderHero();

// Lightbox for screenshots.
const lb = document.getElementById('lightbox');
const lbImg = lb.querySelector('img');
document.querySelectorAll('.zoomable img, .shot-hero img').forEach((img) => {
  img.parentElement.style.cursor = 'zoom-in';
  img.addEventListener('click', () => {
    lbImg.src = img.currentSrc || img.src;
    lbImg.alt = img.alt;
    lb.hidden = false;
  });
});
const closeLb = () => { lb.hidden = true; lbImg.removeAttribute('src'); };
lb.addEventListener('click', closeLb);
document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !lb.hidden) closeLb(); });

// Fade sections in on scroll.
const io = new IntersectionObserver((entries) => {
  entries.forEach((en) => { if (en.isIntersecting) { en.target.classList.add('in'); io.unobserve(en.target); } });
}, { rootMargin: '0px 0px -10% 0px' });
document.querySelectorAll('.feature, .rule, .steps li, .stack-card, .dl, .duo .shot').forEach((el) => {
  el.classList.add('reveal');
  io.observe(el);
});

// Features headline: type out one tool call after another.
const TOOL_CALLS = [
  'npm test',
  'pytest -x',
  'docker build .',
  'go test ./...',
  'cargo build',
  'python train.py',
  'git diff --stat',
  'Read package.json',
  'rg "TODO" src/',
  'terraform plan',
  'WebFetch docs',
  'Agent: write tests',
];
const rotor = document.getElementById('rotor-text');
if (rotor) {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
  let i = 0;
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  (async function loop() {
    for (;;) {
      await sleep(2200);
      const next = TOOL_CALLS[(i = (i + 1) % TOOL_CALLS.length)];
      if (reduced) { rotor.textContent = next; continue; }
      while (rotor.textContent.length) { rotor.textContent = rotor.textContent.slice(0, -1); await sleep(35); }
      await sleep(180);
      for (const ch of next) { rotor.textContent += ch; await sleep(60 + Math.random() * 50); }
    }
  })();
}
