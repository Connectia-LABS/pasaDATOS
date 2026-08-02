(() => {
  "use strict";

  const STORAGE_KEY = "pasadatos.theme.v1";
  const media = window.matchMedia("(prefers-color-scheme: dark)");

  function storedMode() {
    const value = localStorage.getItem(STORAGE_KEY);
    return ["light", "dark", "system"].includes(value) ? value : "system";
  }

  function resolvedTheme(mode) {
    if (mode === "system") return media.matches ? "dark" : "light";
    return mode;
  }

  function applyTheme(mode, persist = true) {
    const safeMode = ["light", "dark", "system"].includes(mode) ? mode : "system";
    const theme = resolvedTheme(safeMode);
    document.documentElement.dataset.theme = theme;
    document.documentElement.dataset.themeMode = safeMode;
    document.documentElement.style.colorScheme = theme;
    if (persist) localStorage.setItem(STORAGE_KEY, safeMode);

    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute("content", theme === "dark" ? "#071426" : "#f4f8fc");

    document.querySelectorAll("[data-theme-option]").forEach((button) => {
      const active = button.dataset.themeOption === safeMode;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", String(active));
    });

    document.querySelectorAll("[data-theme-toggle]").forEach((button) => {
      const next = theme === "dark" ? "light" : "dark";
      button.dataset.nextTheme = next;
      button.setAttribute("aria-label", next === "dark" ? "Activar modo oscuro" : "Activar modo claro");
      button.setAttribute("title", next === "dark" ? "Modo oscuro" : "Modo claro");
      button.querySelectorAll("[data-theme-icon]").forEach((icon) => {
        icon.classList.toggle("hidden", icon.dataset.themeIcon !== next);
      });
    });
  }

  function bind() {
    document.querySelectorAll("[data-theme-option]").forEach((button) => {
      button.addEventListener("click", () => applyTheme(button.dataset.themeOption));
    });
    document.querySelectorAll("[data-theme-toggle]").forEach((button) => {
      button.addEventListener("click", () => applyTheme(button.dataset.nextTheme || "dark"));
    });
    applyTheme(storedMode(), false);
  }

  media.addEventListener?.("change", () => {
    if (storedMode() === "system") applyTheme("system", false);
  });

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", bind, { once: true });
  else bind();
})();
