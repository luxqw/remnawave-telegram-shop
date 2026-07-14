(() => {
  "use strict";

  const root = document.documentElement;
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  /* ---------- Theme toggle ---------- */

  const themeToggle = document.getElementById("themeToggle");
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

  themeToggle.addEventListener("click", () => {
    const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    const current = root.getAttribute("data-theme") || (prefersDark ? "dark" : "light");
    const next = current === "dark" ? "light" : "dark";
    applyTheme(next);
    localStorage.setItem(THEME_KEY, next);
  });

  /* ---------- Segmented tabs ---------- */

  const tabs = Array.from(document.querySelectorAll(".tabs [role='tab']"));
  const indicator = document.getElementById("tabIndicator");
  const docs = {
    agreement: document.getElementById("agreement"),
    privacy: document.getElementById("privacy"),
  };

  function moveIndicator(button) {
    indicator.style.width = button.offsetWidth + "px";
    indicator.style.transform = `translateX(${button.offsetLeft - 4}px)`;
  }

  function selectTab(name, { updateHash = true, scroll = false } = {}) {
    tabs.forEach((btn) => {
      const active = btn.dataset.target === name;
      btn.setAttribute("aria-selected", String(active));
      if (active) moveIndicator(btn);
    });
    Object.entries(docs).forEach(([key, section]) => {
      section.classList.toggle("active", key === name);
    });
    if (updateHash) {
      history.replaceState(null, "", `#${name}`);
    }
    if (scroll) {
      document.querySelector(".tabs").scrollIntoView({ behavior: reduceMotion ? "auto" : "smooth", block: "start" });
    }
  }

  tabs.forEach((btn) => {
    btn.addEventListener("click", () => selectTab(btn.dataset.target, { scroll: true }));
  });

  window.addEventListener("resize", () => {
    const current = tabs.find((b) => b.getAttribute("aria-selected") === "true");
    if (current) moveIndicator(current);
  });

  /* ---------- Deep-link handling (#agreement, #privacy, #agreement-4, ...) ---------- */

  function docNameFromHash(hash) {
    const id = hash.replace("#", "");
    if (docs[id]) return id;
    const match = id.match(/^(agreement|privacy)-/);
    return match ? match[1] : null;
  }

  function handleInitialHash() {
    const name = docNameFromHash(location.hash) || "agreement";
    selectTab(name, { updateHash: false });
    if (location.hash && location.hash !== `#${name}`) {
      const target = document.querySelector(location.hash);
      if (target) requestAnimationFrame(() => target.scrollIntoView({ behavior: "auto", block: "start" }));
    }
  }

  document.querySelectorAll('.toc a[href^="#"]').forEach((link) => {
    link.addEventListener("click", (event) => {
      const hash = link.getAttribute("href");
      const name = docNameFromHash(hash);
      if (name) {
        event.preventDefault();
        selectTab(name, { updateHash: false });
        history.replaceState(null, "", hash);
        requestAnimationFrame(() => {
          document.querySelector(hash).scrollIntoView({ behavior: reduceMotion ? "auto" : "smooth", block: "start" });
        });
      }
    });
  });

  handleInitialHash();

  /* ---------- Active TOC item on scroll ---------- */

  const tocLinks = Array.from(document.querySelectorAll(".toc a"));
  const clauseObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        const id = entry.target.id;
        tocLinks.forEach((link) => {
          link.classList.toggle("current", link.getAttribute("href") === `#${id}`);
        });
      });
    },
    { rootMargin: "-30% 0px -60% 0px", threshold: 0 }
  );

  document.querySelectorAll(".clause[id]").forEach((el) => clauseObserver.observe(el));

  /* ---------- Glass spotlight follow ---------- */

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
