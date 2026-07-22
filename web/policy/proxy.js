(() => {
  "use strict";

  // Fill in real MTProto proxy links (t.me/proxy?server=...&port=...&secret=...) as they become
  // available. Empty strings are skipped; if every entry is empty, the "coming soon" state
  // renders instead. No labels — the visible link text is what tells the 3 cards apart.
  const PROXIES = ["", "", ""];

  const root = document.documentElement;
  const THEME_KEY = "vexel-policy-theme";

  function applyTheme(theme) {
    if (theme === "light" || theme === "dark") {
      root.setAttribute("data-theme", theme);
    } else {
      root.removeAttribute("data-theme");
    }
  }

  const storedTheme = localStorage.getItem(THEME_KEY);
  if (storedTheme) applyTheme(storedTheme);

  document.getElementById("themeToggle").addEventListener("click", () => {
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    const current = root.getAttribute("data-theme") || (prefersDark ? "dark" : "light");
    const next = current === "dark" ? "light" : "dark";
    applyTheme(next);
    localStorage.setItem(THEME_KEY, next);
  });

  const connectIcon = '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7M8 7h9v9"/></svg>';
  const copyIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></svg>';
  const checkIcon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>';

  const activeProxies = PROXIES.filter((link) => link);
  if (activeProxies.length > 0) {
    document.getElementById("proxyPending").style.display = "none";
    const grid = document.getElementById("proxyGrid");

    activeProxies.forEach((link) => {
      const tile = document.createElement("div");
      tile.className = "proxy-tile";

      const linkBtn = document.createElement("button");
      linkBtn.className = "proxy-tile-link";
      linkBtn.type = "button";
      linkBtn.setAttribute("aria-label", "Скопировать ссылку");
      linkBtn.title = "Скопировать ссылку";

      const linkIcon = document.createElement("span");
      linkIcon.setAttribute("aria-hidden", "true");
      linkIcon.innerHTML = copyIcon;

      const linkText = document.createElement("span");
      linkText.textContent = link;

      linkBtn.append(linkIcon, linkText);
      linkBtn.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(link);
          linkIcon.innerHTML = checkIcon;
          linkBtn.classList.add("copied");
          setTimeout(() => {
            linkIcon.innerHTML = copyIcon;
            linkBtn.classList.remove("copied");
          }, 1500);
        } catch {
          // Clipboard API unavailable (older browser/no permission) — the "Подключить" link
          // still works, and the URL text is already selectable by hand as a fallback.
        }
      });

      const connectLink = document.createElement("a");
      connectLink.className = "pill-link primary";
      connectLink.href = link;
      connectLink.target = "_blank";
      connectLink.rel = "noopener";
      connectLink.innerHTML = connectIcon + "<span>Подключить</span>";

      tile.append(linkBtn, connectLink);
      grid.appendChild(tile);
    });
  }

  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (!reduceMotion && window.matchMedia("(pointer: fine)").matches) {
    document.querySelectorAll(".sheet").forEach((sheet) => {
      sheet.addEventListener("pointermove", (event) => {
        const rect = sheet.getBoundingClientRect();
        const x = ((event.clientX - rect.left) / rect.width) * 100;
        const y = ((event.clientY - rect.top) / rect.height) * 100;
        sheet.style.setProperty("--mx", `${x}%`);
        sheet.style.setProperty("--my", `${y}%`);
      });
    });
  }
})();
