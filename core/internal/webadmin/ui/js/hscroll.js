// Horizontal scrollers (.shelf, .tabs, .table-wrap, [data-hscroll]): arrow
// buttons + edge fades shown only when content overflows in that direction.
const SEL = ".shelf, .tabs, .table-wrap, [data-hscroll]";

function enhance(el) {
  if (el.dataset.hsDone) return;
  el.dataset.hsDone = "1";
  const wrap = document.createElement("div");
  wrap.className = "hs-wrap" + (el.classList.contains("tabs") ? " small" : "");
  el.parentNode.insertBefore(wrap, el);
  wrap.appendChild(el);
  el.classList.add("hs-el");
  const btn = (dir) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = `hs-btn ${dir < 0 ? "hs-prev" : "hs-next"}`;
    b.setAttribute("aria-label", dir < 0 ? "向左滚动" : "向右滚动");
    b.textContent = dir < 0 ? "‹" : "›";
    b.addEventListener("click", () => el.scrollBy({ left: dir * Math.max(120, el.clientWidth * 0.9), behavior: "smooth" }));
    wrap.appendChild(b);
  };
  btn(-1); btn(1);
  const update = () => {
    const max = el.scrollWidth - el.clientWidth;
    wrap.classList.toggle("can-l", el.scrollLeft > 2);
    wrap.classList.toggle("can-r", max - el.scrollLeft > 2);
  };
  el.addEventListener("scroll", update, { passive: true });
  if ("ResizeObserver" in window) new ResizeObserver(update).observe(el);
  new MutationObserver(update).observe(el, { childList: true });
  requestAnimationFrame(update);
}

export function initHScroll(root = document.body) {
  root.querySelectorAll(SEL).forEach(enhance);
  new MutationObserver((ms) => {
    for (const m of ms) for (const n of m.addedNodes) {
      if (n.nodeType !== 1) continue;
      if (n.matches(SEL)) enhance(n);
      n.querySelectorAll?.(SEL).forEach(enhance);
    }
  }).observe(root, { childList: true, subtree: true });
}
