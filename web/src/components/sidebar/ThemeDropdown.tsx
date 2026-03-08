import { useState } from "react";

/**
 * All daisyUI 5 built-in themes.
 * @see https://daisyui.com/docs/themes/
 */
const THEMES = [
  "light",
  "dark",
  "cupcake",
  "bumblebee",
  "emerald",
  "corporate",
  "synthwave",
  "retro",
  "cyberpunk",
  "valentine",
  "halloween",
  "garden",
  "forest",
  "aqua",
  "lofi",
  "pastel",
  "fantasy",
  "wireframe",
  "black",
  "luxury",
  "dracula",
  "cmyk",
  "autumn",
  "business",
  "acid",
  "lemonade",
  "night",
  "coffee",
  "winter",
  "dim",
  "nord",
  "sunset",
  "caramellatte",
  "abyss",
  "silk",
] as const;

const STORAGE_KEY = "nekoclaw-theme";

/**
 * Theme switcher dropdown using daisyUI theme-controller.
 * Persists selection to localStorage.
 * Opens upward (dropdown-top) since it sits at the sidebar bottom.
 */
export function ThemeDropdown() {
  const [theme, setTheme] = useState(
    () => localStorage.getItem(STORAGE_KEY) || "dark",
  );

  function handleChange(newTheme: string) {
    setTheme(newTheme);
    localStorage.setItem(STORAGE_KEY, newTheme);
    // Apply theme directly — React controlled inputs prevent
    // daisyUI theme-controller from setting data-theme automatically
    document.documentElement.setAttribute("data-theme", newTheme);
    // Close dropdown by blurring active element
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  }

  return (
    <div className="dropdown dropdown-top w-full">
      <div
        tabIndex={0}
        role="button"
        className="btn btn-ghost w-full justify-start gap-2 h-auto py-2.5 is-drawer-close:btn-square is-drawer-close:tooltip is-drawer-close:tooltip-right"
        data-tip="主題"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          fill="none"
          viewBox="0 0 24 24"
          strokeWidth={1.5}
          stroke="currentColor"
          className="size-4 shrink-0"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            d="M4.098 19.902a3.75 3.75 0 0 0 5.304 0l6.401-6.402M6.75 21A3.75 3.75 0 0 1 3 17.25V4.125C3 3.504 3.504 3 4.125 3h5.25c.621 0 1.125.504 1.125 1.125v4.072M6.75 21a3.75 3.75 0 0 0 3.75-3.75V8.197M6.75 21h13.125c.621 0 1.125-.504 1.125-1.125v-5.25c0-.621-.504-1.125-1.125-1.125h-4.072M10.5 8.197l2.88-2.88c.438-.439 1.15-.439 1.59 0l3.712 3.713c.44.44.44 1.152 0 1.59l-2.879 2.88M6.75 17.25h.008v.008H6.75v-.008Z"
          />
        </svg>
        <span className="is-drawer-close:hidden capitalize">{theme}</span>
      </div>
      <ul
        tabIndex={0}
        className="dropdown-content bg-base-300 rounded-box z-50 w-52 p-2 shadow-2xl max-h-80 overflow-y-auto [scrollbar-width:thin]"
      >
        {THEMES.map((t) => (
          <li key={t}>
            <input
              type="radio"
              name="theme-dropdown"
              className="theme-controller w-full btn btn-sm btn-block btn-ghost justify-start"
              aria-label={t}
              value={t}
              checked={theme === t}
              onChange={() => handleChange(t)}
            />
          </li>
        ))}
      </ul>
    </div>
  );
}
