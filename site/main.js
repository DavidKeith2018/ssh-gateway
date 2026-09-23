const repository = window.SSH_GATEWAY_REPOSITORY;
if (repository && /^https:\/\/github\.com\/[\w.-]+\/[\w.-]+$/.test(repository)) {
  document.querySelectorAll('[data-repo-path]').forEach(link => {
    link.href = `${repository}/${link.dataset.repoPath}`;
    link.hidden = false;
  });
}

const translations = window.SSH_GATEWAY_TRANSLATIONS;
const localizedNodes = Object.keys(translations.en.content).flatMap(selector =>
  [...document.querySelectorAll(selector)].map(node => ({ node, selector }))
);
let language = 'en';
let copyStatus = '';
function messages() { return translations[language]; }
function applyLanguage(next) {
  language = next === 'en' ? 'en' : 'zh';
  const text = messages();
  document.documentElement.lang = language === 'zh' ? 'zh-CN' : 'en';
  document.title = text.title;
  document.querySelector('meta[name="description"]').content = text.description;
  document.querySelector('nav').setAttribute('aria-label', text.navigation);
  document.querySelector('.console').setAttribute('aria-label', text.console);
  localizedNodes.forEach(({node, selector}) => {
    // Translations come only from the bundled static catalog.
    node.innerHTML = translations[language].content[selector];
  });
  document.querySelectorAll('[data-alt-zh]').forEach(node => { node.alt = language === 'zh' ? node.dataset.altZh : node.dataset.altEn; });
  document.querySelectorAll('[data-language]').forEach(button => {
    button.setAttribute('aria-pressed', String(button.dataset.language === language));
  });
  document.querySelector('#copy-status').textContent = copyStatus ? text[copyStatus] : '';
}
function preferredLanguage() {
  const explicit = new URL(location.href).searchParams.get('lang');
  if (explicit === 'zh' || explicit === 'en') return explicit;
  try {
    const saved = localStorage.getItem('ssh-gateway-language');
    if (saved === 'zh' || saved === 'en') return saved;
  } catch { /* Language selection works without storage. */ }
  return (navigator.languages?.[0] || navigator.language || 'en').toLowerCase().startsWith('zh') ? 'zh' : 'en';
}
document.querySelectorAll('[data-language]').forEach(button => {
  button.addEventListener('click', () => {
    applyLanguage(button.dataset.language);
    try { localStorage.setItem('ssh-gateway-language', language); } catch { /* Storage is optional. */ }
    const url = new URL(location.href);
    url.searchParams.set('lang', language);
    history.replaceState(null, '', url);
  });
});
applyLanguage(preferredLanguage());

document.querySelector('#copy').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(document.querySelector('#commands').textContent);
    copyStatus = 'copied';
  } catch {
    copyStatus = 'copyFailed';
  }
  document.querySelector('#copy-status').textContent = messages()[copyStatus];
});
