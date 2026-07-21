(() => {
  "use strict";

  // Fill in real MTProto proxy links (t.me/proxy?server=...&port=...&secret=...) as they become
  // available. Any entry with an empty link is skipped; if every entry is empty, the "coming
  // soon" state renders instead. Add or remove entries freely - the list isn't fixed at 3.
  const PROXIES = [
    { label: "Прокси 1", link: "" },
    { label: "Прокси 2", link: "" },
    { label: "Прокси 3", link: "" },
  ];

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

  const activeProxies = PROXIES.filter((p) => p.link);
  if (activeProxies.length > 0) {
    document.getElementById("proxyPending").style.display = "none";
    const list = document.getElementById("proxyList");

    activeProxies.forEach((proxy) => {
      const row = document.createElement("div");
      row.className = "proxy-row";

      const head = document.createElement("div");
      head.className = "proxy-row-head";

      const label = document.createElement("div");
      label.className = "proxy-row-label";
      label.textContent = proxy.label;

      const actions = document.createElement("div");
      actions.className = "proxy-row-actions";

      const connectLink = document.createElement("a");
      connectLink.className = "pill-link primary";
      connectLink.href = proxy.link;
      connectLink.target = "_blank";
      connectLink.rel = "noopener";
      connectLink.textContent = "Подключить";

      const copyBtn = document.createElement("button");
      copyBtn.className = "pill-link";
      copyBtn.type = "button";
      copyBtn.textContent = "Скопировать";
      copyBtn.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(proxy.link);
          const original = copyBtn.textContent;
          copyBtn.textContent = "Скопировано";
          setTimeout(() => { copyBtn.textContent = original; }, 1500);
        } catch {
          // Clipboard API unavailable (older browser/no permission) — the link text below is
          // already selectable by hand as a fallback.
        }
      });

      actions.append(connectLink, copyBtn);
      head.append(label, actions);

      const linkText = document.createElement("div");
      linkText.className = "proxy-row-link";
      linkText.textContent = proxy.link;

      row.append(head, linkText);
      list.appendChild(row);
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
