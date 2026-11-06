import { useLayoutEffect, useRef } from "react";

import { slideBox, type Box } from "../lib/nav-slider.ts";

/** Satırın (menü öğesinin) kap içindeki kutusu; kabın kenarlığı ve kaydırması dahil. */
function measure(box: HTMLElement, item: Element): Box {
  return slideBox(box.getBoundingClientRect(), { top: box.clientTop, left: box.clientLeft }, item.getBoundingClientRect(), { top: box.scrollTop, left: box.scrollLeft });
}

function place(el: HTMLElement, b: Box): void {
  el.style.transform = `translate(${b.x}px, ${b.y}px)`;
  el.style.width = `${b.width}px`;
  el.style.height = `${b.height}px`;
}

/** Kayma bir sonraki karede açılır: bu karedeki yerleşim kaymadan olur. */
function slideFromNextFrame(el: HTMLElement): void {
  requestAnimationFrame(() => {
    el.dataset.ready = "";
  });
}

/** Seçili satırın camını seçili satıra taşır; `slide` değilse kaymadan oturur. */
function followActive(el: HTMLElement, box: HTMLElement, slide: boolean): void {
  const item = box.querySelector(".nav-item.is-active");
  if (!item) {
    el.classList.remove("is-on");
    return;
  }
  if (!slide) delete el.dataset.ready;
  place(el, measure(box, item));
  el.classList.add("is-on");
  slideFromNextFrame(el);
}

const sizeOf = (box: HTMLElement): string => `${box.clientWidth}x${box.clientHeight}`;

/**
 * Kenar çubuğunun göstergeleri (K-163, kullanıcının seçtiği 7): seçili satırın
 * camı yeni satıra kayar, üstüne gelme vurgusu fareyi satırdan satıra kayarak
 * izler. İkisi de menünün kabında (`.rail`, `.subnav`), satırların arkasında
 * durur. İlk yerleşimde, pencere boyutu değişince ve "hareketi azalt" açıkken
 * kayma yoktur.
 */
export function NavSlider({ activeKey }: { activeKey: string }) {
  const pill = useRef<HTMLSpanElement>(null);
  const hover = useRef<HTMLSpanElement>(null);

  // Seçim değişince cam yeni satıra kayar (ilk yerleşimde kaymaz: data-ready henüz yok).
  useLayoutEffect(() => {
    const el = pill.current;
    const box = el?.parentElement;
    if (el && box) followActive(el, box, true);
  }, [activeKey]);

  // Pencere boyutu değişince (dar pencere) cam kaymadan yerine oturur; vurgu
  // gizlenir ve sıfır boya iner: saydam da olsa eski boyuyla kabı taşırırdı.
  // Yalnız boyut gerçekten değişince: izleyici kurulurken de bir kez haber verir.
  // Pencere olayı da dinlenir: gizli pencerede boyut izleyicisi gecikebilir.
  useLayoutEffect(() => {
    const el = pill.current;
    const mark = hover.current;
    const box = el?.parentElement;
    if (!el || !mark || !box) return;
    let size = sizeOf(box);
    const resized = (): void => {
      const now = sizeOf(box);
      if (now === size) return;
      size = now;
      followActive(el, box, false);
      mark.classList.remove("is-on");
      delete mark.dataset.ready;
      place(mark, { x: 0, y: 0, width: 0, height: 0 });
    };
    const watch = new ResizeObserver(resized);
    watch.observe(box);
    window.addEventListener("resize", resized);
    return () => {
      watch.disconnect();
      window.removeEventListener("resize", resized);
    };
  }, []);

  useLayoutEffect(() => {
    const el = hover.current;
    const box = el?.parentElement;
    if (!el || !box) return;
    const over = (event: PointerEvent): void => {
      const item = event.target instanceof Element ? event.target.closest(".nav-item:not(:disabled)") : null;
      if (!item || !box.contains(item)) {
        el.classList.remove("is-on");
        return;
      }
      // Gizliyken yeni satırda kaymadan belirir; görünürken satırdan satıra kayar.
      const appearing = !el.classList.contains("is-on");
      if (appearing) delete el.dataset.ready;
      place(el, measure(box, item));
      el.classList.add("is-on");
      if (appearing) slideFromNextFrame(el);
    };
    const leave = (): void => el.classList.remove("is-on");
    box.addEventListener("pointerover", over);
    box.addEventListener("pointerleave", leave);
    return () => {
      box.removeEventListener("pointerover", over);
      box.removeEventListener("pointerleave", leave);
    };
  }, []);

  return (
    <>
      <span ref={hover} className="nav-hover" aria-hidden="true" />
      <span ref={pill} className="nav-pill" aria-hidden="true" />
    </>
  );
}
