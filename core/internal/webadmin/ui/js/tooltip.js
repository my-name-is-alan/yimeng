// App-styled tooltips replacing native title tooltips. Any element with a
// title (set anywhere) has it moved to data-tip on first hover, so the
// browser's own tooltip never shows; text is set via textContent (no HTML).
let tip, cur, timer;

function hide() { clearTimeout(timer); cur = null; if (tip) tip.hidden = true; }

function show(el) {
  const text = el.dataset.tip;
  if (!text) return hide();
  if (!tip) { tip = document.createElement("div"); tip.className = "ym-tip"; tip.setAttribute("role", "tooltip"); document.body.append(tip); }
  tip.textContent = text;
  tip.hidden = false;
  const r = el.getBoundingClientRect(), t = tip.getBoundingClientRect();
  let top = r.top - t.height - 8;
  if (top < 4) top = r.bottom + 8;
  const left = Math.min(Math.max(4, r.left + r.width / 2 - t.width / 2), innerWidth - t.width - 4);
  tip.style.top = `${top}px`; tip.style.left = `${left}px`;
}

function adopt(el) {
  const t = el.getAttribute("title");
  if (t !== null) {
    el.removeAttribute("title");
    if (t) { el.dataset.tip = t; if (!el.hasAttribute("aria-label") && !el.textContent.trim()) el.setAttribute("aria-label", t); }
    else delete el.dataset.tip;
  }
}

export function initTooltips() {
  document.addEventListener("mouseover", (e) => {
    const el = e.target.closest?.("[title],[data-tip]");
    if (!el) return hide();
    adopt(el);
    if (el === cur) return;
    hide(); cur = el;
    timer = setTimeout(() => cur === el && el.isConnected && show(el), 350);
  });
  document.addEventListener("focusin", (e) => { const el = e.target.closest?.("[title],[data-tip]"); if (el) { adopt(el); cur = el; show(el); } });
  for (const ev of ["focusout", "scroll", "mousedown", "keydown"]) document.addEventListener(ev, hide, true);
  // keep title-less: re-adopt titles that code sets later on known elements
  new MutationObserver((ms) => { for (const m of ms) if (m.target.nodeType === 1 && m.target.hasAttribute("title")) adopt(m.target); })
    .observe(document.body, { attributes: true, attributeFilter: ["title"], subtree: true });
}
