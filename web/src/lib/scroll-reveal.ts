// 滚动条"滚才显形"的唯一状态源：滚动中的元素挂上 data-scrolling="true"，停手
// SCROLL_REVEAL_MS 之后摘掉。CSS 只认这个属性（styles.css 里 --scroll-thumb 那一节），
// JS 不直接改样式，也不给每个滚动容器单独绑监听。
export const SCROLL_REVEAL_MS = 700

// 一个容器一条计时器：页面滚动和表格里的笔记格同时在滚时，先停下的那个不能把
// 另一个的滑块一起藏掉。WeakMap 让元素被 React 卸载之后，句柄跟着可回收。
const timers = new WeakMap<Element, ReturnType<typeof setTimeout>>()

function handleScroll(event: Event) {
  const target = event.target
  // 视口滚动的 target 是 document 而不是 Element；本站可滚动的都是元素级容器。
  if (!(target instanceof Element)) return
  // 走属性 API 而不是 dataset：滚动容器未必是 HTMLElement，Element 上没有 dataset。
  target.setAttribute("data-scrolling", "true")
  const pending = timers.get(target)
  if (pending !== undefined) clearTimeout(pending)
  timers.set(
    target,
    setTimeout(() => {
      target.removeAttribute("data-scrolling")
      timers.delete(target)
    }, SCROLL_REVEAL_MS),
  )
}

// 捕获阶段 + window：scroll 不冒泡，只有在 window 上开 capture 才收得到元素级滚动，
// 于是不必知道有哪些滚动容器，也不受 React 增删节点影响。passive 是因为这里只写属性。
// handleScroll 是模块级单例，重复 install 会被 addEventListener 自己去重。
export function installScrollReveal(scope: Window = window): () => void {
  scope.addEventListener("scroll", handleScroll, { capture: true, passive: true })
  return () => scope.removeEventListener("scroll", handleScroll, { capture: true })
}
