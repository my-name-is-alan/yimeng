// Custom dropdown that replaces every native <select> in the app.
// The native element stays in the DOM (hidden) as the source of truth, so
// existing code that reads .value, listens for "change" or rebuilds options
// keeps working; we mirror it into a styled button + popover listbox.
// Keyboard: Enter/Space/ArrowDown open; arrows/Home/End move; type-ahead;
// Enter selects; Escape/Tab close.
let seq = 0;
let openPop = null;

function closeOpen() { if (openPop) openPop(); }

export function enhanceSelect(sel) {
  if (sel.dataset.ymSelect || sel.multiple || sel.size > 1) return;
  sel.dataset.ymSelect = "1";
  const id = `yms-${++seq}`;
  const wrap = document.createElement("div");
  wrap.className = "ym-select";
  if (sel.style.width) { wrap.style.width = sel.style.width; }
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ym-select-btn input";
  btn.setAttribute("aria-haspopup", "listbox");
  btn.setAttribute("aria-expanded", "false");
  btn.setAttribute("aria-controls", id);
  const lab = sel.getAttribute("aria-label"); if (lab) btn.setAttribute("aria-label", lab);
  const val = document.createElement("span"); val.className = "ym-select-val";
  const chev = document.createElement("span"); chev.className = "ym-select-chev"; chev.setAttribute("aria-hidden", "true");
  btn.append(val, chev);
  const list = document.createElement("ul");
  list.className = "ym-select-list"; list.id = id; list.setAttribute("role", "listbox"); list.tabIndex = -1; list.hidden = true;
  sel.parentNode.insertBefore(wrap, sel);
  wrap.append(sel, btn, list);
  sel.classList.add("ym-native");
  sel.tabIndex = -1; sel.setAttribute("aria-hidden", "true");

  let active = -1, typed = "", typedAt = 0;
  const opts = () => [...sel.options];
  const sync = () => {
    const o = sel.options[sel.selectedIndex];
    val.textContent = o ? o.textContent : "";
    btn.disabled = sel.disabled;
    if (sel.labels?.[0] && !lab) btn.setAttribute("aria-label", sel.labels[0].textContent);
  };
  const render = () => {
    list.replaceChildren(...opts().map((o, i) => {
      const li = document.createElement("li");
      li.id = `${id}-${i}`; li.setAttribute("role", "option"); li.textContent = o.textContent;
      li.setAttribute("aria-selected", String(i === sel.selectedIndex));
      if (o.disabled) li.setAttribute("aria-disabled", "true");
      li.addEventListener("mousedown", (e) => e.preventDefault());
      li.addEventListener("click", () => { if (!o.disabled) choose(i); });
      li.addEventListener("mousemove", () => setActive(i));
      return li;
    }));
  };
  const setActive = (i) => {
    const n = sel.options.length; if (!n) return;
    active = Math.max(0, Math.min(n - 1, i));
    [...list.children].forEach((li, k) => li.classList.toggle("active", k === active));
    btn.setAttribute("aria-activedescendant", `${id}-${active}`);
    list.children[active]?.scrollIntoView({ block: "nearest" });
  };
  const choose = (i) => {
    if (sel.selectedIndex !== i) {
      sel.selectedIndex = i;
      sel.dispatchEvent(new Event("input", { bubbles: true }));
      sel.dispatchEvent(new Event("change", { bubbles: true }));
    }
    sync(); close(); btn.focus();
  };
  const place = () => {
    const r = btn.getBoundingClientRect();
    const below = window.innerHeight - r.bottom, h = Math.min(list.scrollHeight, 300);
    wrap.classList.toggle("up", below < h + 12 && r.top > below);
  };
  const open = () => {
    if (btn.disabled || !list.hidden) return;
    closeOpen();
    render(); list.hidden = false; wrap.classList.add("open");
    btn.setAttribute("aria-expanded", "true");
    place(); setActive(sel.selectedIndex < 0 ? 0 : sel.selectedIndex);
    openPop = close;
  };
  function close() {
    if (list.hidden) return;
    list.hidden = true; wrap.classList.remove("open", "up");
    btn.setAttribute("aria-expanded", "false"); btn.removeAttribute("aria-activedescendant");
    if (openPop === close) openPop = null;
  }
  btn.addEventListener("click", () => (list.hidden ? open() : close()));
  btn.addEventListener("blur", () => setTimeout(() => { if (!wrap.contains(document.activeElement)) close(); }, 0));
  btn.addEventListener("keydown", (e) => {
    const isOpen = !list.hidden;
    switch (e.key) {
      case "ArrowDown": e.preventDefault(); isOpen ? setActive(active + 1) : open(); break;
      case "ArrowUp": e.preventDefault(); isOpen ? setActive(active - 1) : open(); break;
      case "Home": if (isOpen) { e.preventDefault(); setActive(0); } break;
      case "End": if (isOpen) { e.preventDefault(); setActive(sel.options.length - 1); } break;
      case "Enter": case " ": e.preventDefault(); isOpen ? (active >= 0 && !sel.options[active]?.disabled && choose(active)) : open(); break;
      case "Escape": if (isOpen) { e.preventDefault(); e.stopPropagation(); close(); } break;
      case "Tab": close(); break;
      default:
        if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
          const now = Date.now(); typed = (now - typedAt > 700 ? "" : typed) + e.key.toLowerCase(); typedAt = now;
          const i = opts().findIndex((o) => o.textContent.toLowerCase().startsWith(typed));
          if (i >= 0) { if (isOpen) setActive(i); else choose(i); }
        }
    }
  });
  // Mirror programmatic changes: options rebuilt, value set, disabled toggled.
  new MutationObserver(() => { sync(); if (!list.hidden) render(); }).observe(sel, { childList: true, subtree: true, attributes: true, characterData: true });
  sel.addEventListener("change", sync);
  const desc = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value");
  Object.defineProperty(sel, "value", { configurable: true, get() { return desc.get.call(this); }, set(v) { desc.set.call(this, v); sync(); } });
  const di = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "selectedIndex");
  Object.defineProperty(sel, "selectedIndex", { configurable: true, get() { return di.get.call(this); }, set(v) { di.set.call(this, v); sync(); } });
  sync();
}

// Enhance every <select> that enters the document, now and later.
export function initSelects(root = document.body) {
  root.querySelectorAll("select").forEach(enhanceSelect);
  new MutationObserver((muts) => {
    for (const m of muts) for (const n of m.addedNodes) {
      if (n.nodeType !== 1) continue;
      if (n.tagName === "SELECT") enhanceSelect(n);
      else n.querySelectorAll?.("select").forEach(enhanceSelect);
    }
  }).observe(root, { childList: true, subtree: true });
  document.addEventListener("mousedown", (e) => { if (openPop && !e.target.closest?.(".ym-select.open")) closeOpen(); });
  window.addEventListener("resize", closeOpen);
}
