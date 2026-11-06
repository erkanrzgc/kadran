/**
 * Kenar çubuğunun kayan göstergesi için konum hesabı (K-163, kullanıcının
 * seçtiği 7). Saf: DOM'a dokunmaz, ölçülen dikdörtgenlerden kutu üretir.
 */

export interface RectLike {
  top: number;
  left: number;
  width: number;
  height: number;
}

export interface Edge {
  top: number;
  left: number;
}

export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

/**
 * slideBox, göstergenin kap içindeki kutusu: satırın ekrandaki yeri eksi kabın
 * dolgu kutusunun yeri (kenarlık düşülür), artı kaydırma. Gösterge kabın
 * içinde mutlak konumlu olduğu için içerikle birlikte kayar.
 */
export function slideBox(container: RectLike, border: Edge, item: RectLike, scroll: Edge): Box {
  return {
    x: item.left - container.left - border.left + scroll.left,
    y: item.top - container.top - border.top + scroll.top,
    width: item.width,
    height: item.height,
  };
}
